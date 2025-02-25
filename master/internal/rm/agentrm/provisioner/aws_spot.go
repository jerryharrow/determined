package provisioner

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/determined-ai/determined/master/internal/config/provconfig"
	"github.com/determined-ai/determined/master/pkg/actor"
	"github.com/determined-ai/determined/master/pkg/model"
)

const (
	spotRequestIDPrefix    = "sir-"
	launchTimeOffsetGrowth = time.Second * 10
)

type spotRequest struct {
	SpotRequestID string
	State         string
	StatusCode    *string
	StatusMessage *string
	InstanceID    *string
	CreationTime  time.Time
}

// How Spot Works:
//
// Spot instances are created asynchronously. You create a spot request, the
// request is validated and, if there is available capacity at the given price,
// an instance will be created (spot request fulfilled). We use one-time spot
// requests rather than persistent requests - if an instance is shut down, the
// spot request will not try to automatically launch a new instance. We do this
// so state management is slightly simpler because AWS will not be doing any
// provisioning outside of our code that we need to account for.
//
// Once the spot request has been fulfilled, the request in the API will have a
// pointer to the instance id. If the spot request is canceled, the instance will
// continue to run. The spot request will have the status
// "request-canceled-and-instance-running". If the instance is stopped or terminated,
// either manually or automatically by AWS, the spot request will enter a terminal
// state (either canceled, closed or disabled).
//
// One major issue this code handles is that the Spot Request API is eventually
// consistent and there may be a 30 second delay between creating a spot request and
// having it visible in listSpotRequests. We maintain an internal list of the spot
// requests we've created to prevent overprovisioning.
//
// The other major issue is that, when creating a spot request, you must pass in a
// "validFrom" parameter. This is a timestamp that tells AWS not to attempt to fulfill
// the request before this time. This time must be in the future or the request will be
// rejected as having bad params. However, the timestamp must be generated by our code
// locally and is then evaluated by the AWS API. Their clocks may not match our clocks
// so a time that we think is 10 seconds in the future could be in the past or
// potentially hours in the future. We try to account for any potential differences
// in clocks when generating the validFrom timestamp. More detail can be found in the
// spotRequest struct documentation below.
//
// In some cases spot requests will not be able to be fulfilled. Some errors may
// be permanently fatal (e.g. AWS does not have the instance type in this AZ) and
// requires user interaction to fix. In other cases, the error is transient (e.g.
// AWS account limits hit, internal system error) and may disappear without user
// interaction, but the user should be made aware of them because the user may be
// able to intervene and solve the problem. It is not clear how to differentiate
// these cases, so we handle them identically.
//
// AWS documentation on the spot instance lifecycle:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-request-status.html#spot-instance-bid-status-understand
//
//nolint:lll
type spotState struct {
	// Keep track of spot requests that haven't entered a terminal state. This map primarily
	// exists to handle problems caused by eventual consistency, where we will create a spot
	// request but it won't be visible in the AWS API so if we rely solely on the API, we
	// will think we need to create additional spot requests, leading to overprovisioning.
	trackedReqs setOfSpotRequests

	// When creating a spot request, the validFrom time needs be in the future when evaluated by
	// the AWS API (otherwise the request will be rejected by AWS with a 'bad-param' error).
	// We can't rely on our clocks being in sync with AWS's. We try to approximate the clock
	// skew by creating an spot request when the provisioner is instantiated and comparing
	// time.Now() when we create the request to the timestamp that AWS records for request
	// creation. We use this value to adjust time.Now() in our code to match AWS. If that
	// approximation fails (e.g. we can't create the spot request), we assume that
	// approximateClockSkew = 0. This is a safe assumption because we also have launchTimeOffset
	// to handle the clock skew problem. However, only using launchTimeOffset may lead to
	// a longer than desired wait before a spot instance request gets fulfilled, if the local
	// clock is ahead of AWS.
	approximateClockSkew time.Duration

	// When creating a spot requests, we set the validFrom field to be time.Now() +
	// approximateClockSkew + launchTimeOffset. If clocks were perfectly synced and API calls
	// had no latency, we would want launchTimeOffset to be tiny so that the request
	// would start being fulfilled immediately after the spot request is submitted. However
	// API calls do have latency and there will be clock skew (and the best we can do is
	// approximate that skew). By default we set the validFrom field to be 10 seconds in the
	// future. If AWS rejects this time due to it not being in the future, we increase the
	// launchTimeOffset. If we do this enough times, we will start generating validFrom times
	// that are in the future according to AWS. One clock skew problem that is not fixed by
	// this is: if the local clock is ahead of the AWS clocks, our validFrom time may be quite
	// far in the future and AWS won't try to fulfill it until that time is reached. This is
	// why the approximateClockSkew measurement is needed.
	launchTimeOffset time.Duration
}

// listSpot lists all unfulfilled and fulfilled spot requests. If the spot request has been
// fulfilled an actual EC2 instance will be returned as an Instance. If the request has not
// been fulfilled, a fake Instance will be returned where the InstanceID is the SpotRequestID
// and the state is SpotRequestPendingAWS.
//
// This function does more than just list spot instances. Because this function is called every
// provisioner tick, we have it also handle several aspects of the spot provisioner lifecycle.
func (c *awsCluster) listSpot(ctx *actor.Context) ([]*model.Instance, error) {
	activeReqsInAPI, err := c.listActiveSpotInstanceRequests(ctx, false)
	if err != nil {
		return nil, errors.Wrap(err, "cannot describe EC2 spot requests")
	}

	// Make sure we're tracking all active reqs (to handle master restart)
	for _, req := range activeReqsInAPI.iter() {
		c.spot.trackedReqs.add(req)
	}

	err = c.setTagsOnInstances(ctx, activeReqsInAPI)
	if err != nil {
		ctx.Log().
			WithError(err).
			Error("unable to create tags on ec2 instances created by spot")
	}

	reqsToNotifyUserAbout := newSetOfSpotRequests()
	for _, req := range activeReqsInAPI.iter() {
		switch *req.StatusCode {
		case
			"capacity-not-available",
			"price-too-low":
			reqsToNotifyUserAbout.add(req)
		}
	}

	// If there are requests that we are tracking, but didn't get returned
	// in listActiveSpotInstanceRequests, query the API for them specifically.
	// They could be fresh requests that aren't visible in the API yet or they
	// could have entered an inactive state for some reason.
	missingReqs := c.spot.trackedReqs.copy()
	missingReqs.deleteIntersection(*activeReqsInAPI)

	newOrInactiveReqs, err := c.listSpotRequestsByID(ctx, missingReqs.idsAsListOfPointers(), false)
	if err != nil {
		return nil, errors.Wrap(err, "cannot describe EC2 spot requests")
	}

	// If any of the tracked requests failed and requires users intervention, notify the user via error
	// logs. Also stop tracking any request that is in a terminal state.
	numReqsNoLongerTracked := 0
	for _, req := range newOrInactiveReqs.iter() {
		missingReqs.delete(req)
		if req.State != "active" && req.State != "open" {
			c.spot.trackedReqs.delete(req)
			numReqsNoLongerTracked++
		}
		switch *req.StatusCode {
		case
			"bad-parameters",
			"constraint-not-fulfillable",
			"limit-exceeded":
			reqsToNotifyUserAbout.add(req)
		}
	}

	for _, req := range reqsToNotifyUserAbout.asListInChronologicalOrder() {
		ctx.Log().
			WithField("spot-request-status-code", *req.StatusCode).
			WithField("spot-request-status-message", *req.StatusMessage).
			WithField("spot-request-creation-time", req.CreationTime.String()).
			Error("a spot request cannot be fulfilled and may require user intervention")
	}

	// Canonical log line for debugging
	ctx.Log().
		WithField("log-type", "listSpot.summary").
		WithField("total-num-requests-being-tracked", c.spot.trackedReqs.numReqs()).
		WithField("num-visible-as-active-in-api", activeReqsInAPI.numReqs()).
		WithField("num-tracked-but-not-visible-in-aws-api", missingReqs.numReqs()).
		WithField("num-no-longer-tracked-due-to-terminal-state", numReqsNoLongerTracked).
		Debugf("updated the list of active spot requests being tracked. "+
			"there are %d spot requests being tracked. the following requests "+
			"are being tracked but aren't visible in AWS yet: %s",
			c.spot.trackedReqs.numReqs(),
			missingReqs.idsAsList())

	// Cleanup CanceledButInstanceRunningRequests because an instance could have been
	// created between listing and terminating spot requests.
	canceledButInstanceRunningReqs, err := c.listCanceledButInstanceRunningSpotRequests(ctx, false)
	if err != nil {
		return nil, errors.Wrap(err, "cannot describe EC2 spot requests")
	}

	if canceledButInstanceRunningReqs.numReqs() > 0 {
		ctx.Log().Debugf(
			"terminating EC2 instances associated with canceled spot requests: %s",
			strings.Join(canceledButInstanceRunningReqs.idsAsList(), ","),
		)
		_, err = c.terminateInstances(canceledButInstanceRunningReqs.instanceIds())
		if err != nil {
			ctx.Log().
				WithError(err).
				Debugf("cannot terminate EC2 instances associated with canceled spot requests")
		}
	}

	instances, err := c.buildInstanceListFromTrackedReqs(ctx)
	if err != nil {
		return nil, err
	}
	return instances, nil
}

func (c *awsCluster) terminateSpot(ctx *actor.Context, instanceIDs []*string) {
	if len(instanceIDs) == 0 {
		return
	}

	instancesToTerminate := newSetOfStrings()
	pendingSpotReqsToTerminate := newSetOfStrings()

	for _, instanceID := range instanceIDs {
		if strings.HasPrefix(*instanceID, spotRequestIDPrefix) {
			spotRequestID := instanceID
			pendingSpotReqsToTerminate.add(*spotRequestID)
		} else {
			instancesToTerminate.add(*instanceID)
		}
	}

	ctx.Log().
		WithField("log-type", "terminateSpot.start").
		Debugf(
			"terminating %d EC2 instances and %d spot requests: %s,  %s",
			instancesToTerminate.length(),
			pendingSpotReqsToTerminate.length(),
			instancesToTerminate.string(),
			pendingSpotReqsToTerminate.string(),
		)

	if instancesToTerminate.length() > 0 {
		ctx.Log().Infof(
			"terminating EC2 instances associated with fulfilled spot requests: %s",
			instancesToTerminate.string(),
		)
		c.terminateOnDemand(ctx, instancesToTerminate.asListOfPointers())
	}

	_, err := c.terminateSpotInstanceRequests(
		ctx, pendingSpotReqsToTerminate.asListOfPointers(), false,
	)
	if err != nil {
		ctx.Log().WithError(err).Error("cannot terminate spot requests")
	} else {
		ctx.Log().
			WithField("log-type", "terminateSpot.terminatedSpotRequests").
			Debugf(
				"terminated %d spot requests: %s",
				pendingSpotReqsToTerminate.length(),
				pendingSpotReqsToTerminate.string(),
			)
	}
}

func (c *awsCluster) launchSpot(
	ctx *actor.Context,
	instanceNum int,
) error {
	if instanceNum <= 0 {
		return nil
	}

	ctx.Log().
		WithField("log-type", "launchSpot.start").
		Infof("launching %d EC2 spot requests", instanceNum)
	resp, err := c.createSpotInstanceRequestsCorrectingForClockSkew(ctx, instanceNum, false)
	if err != nil {
		ctx.Log().WithError(err).Error("cannot launch EC2 spot requests")
		return err
	}

	// Update the internal spotRequest tracker because there can be a large delay
	// before the API starts including these requests in listSpotRequest API calls,
	// and if we don't track it internally, we will end up overprovisioning.
	for _, request := range resp.SpotInstanceRequests {
		c.spot.trackedReqs.add(&spotRequest{
			SpotRequestID: *request.SpotInstanceRequestId,
			State:         *request.State,
			StatusCode:    request.Status.Code,
			StatusMessage: request.Status.Message,
			CreationTime:  *request.CreateTime,
			InstanceID:    nil,
		})

		ctx.Log().
			WithField("log-type", "launchSpot.creatingRequest").
			Infof(
				"creating spot request: %s (state %s)",
				*request.SpotInstanceRequestId,
				*request.State,
			)
	}
	return nil
}

func (c *awsCluster) setTagsOnInstances(ctx *actor.Context, activeReqs *setOfSpotRequests) error {
	instanceIDs := activeReqs.instanceIds()
	if len(instanceIDs) == 0 {
		return nil
	}

	input := &ec2.CreateTagsInput{
		Resources: instanceIDs,
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(c.InstanceName),
			},
			{
				Key:   aws.String("determined-resource-pool"),
				Value: aws.String(c.resourcePool),
			},
			{
				Key:   aws.String(c.TagKey),
				Value: aws.String(c.TagValue),
			},
			{
				Key:   aws.String("determined-master-address"),
				Value: aws.String(c.masterURL.String()),
			},
		},
	}
	_, err := c.client.CreateTags(input)
	return err
}

// Create a spot request to try to approximate how different the local clock is
// from the AWS API clock. Record the local time.Now(), create a spot requests,
// then inspect the timestamp that AWS returns as the createTime. This will
// approximately tell us how different the AWS clock is from the local clock. It
// will also include the time it takes from creating the request to AWS receiving
// the request, but that is fine. Finally, the function will delete that spot
// request so it isn't fulfilled.
func (c *awsCluster) attemptToApproximateClockSkew(ctx *actor.Context) {
	ctx.Log().Debug("new AWS spot provisioner. launching spot request to determined approximate " +
		"clock skew between local machine and AWS API.")
	localCreateTime := time.Now()
	resp, err := c.createSpotInstanceRequest(ctx, 1, c.AWSClusterConfig.InstanceType,
		time.Hour*100, false)
	if err != nil {
		ctx.Log().
			WithError(err).
			Infof("error while launching spot request during clock skew approximation. Non-fatal error, " +
				"defaulting to assumption that AWS clock and local clock have minimal clock skew")
		return
	}
	awsCreateTime := resp.SpotInstanceRequests[0].CreateTime
	approxClockSkew := awsCreateTime.Sub(localCreateTime)
	ctx.Log().Infof("AWS API clock is approximately %s ahead of local machine clock",
		approxClockSkew.String())
	for {
		ctx.Log().Debugf("attempting to clean up spot request used to approximate clock skew")
		_, err = c.terminateSpotInstanceRequests(ctx,
			[]*string{resp.SpotInstanceRequests[0].SpotInstanceRequestId},
			false)
		if err == nil {
			ctx.Log().Debugf("Successfully cleaned up spot request used to approximate clock skew")
			break
		}
		if awsErr, ok := err.(awserr.Error); ok {
			ctx.Log().
				Debugf(
					"AWS error while terminating spot request used for clock skew approximation, %s, %s",
					awsErr.Code(),
					awsErr.Message())
			if awsErr.Code() != "InvalidSpotInstanceRequestID.NotFound" {
				return
			}
		} else {
			ctx.Log().Errorf("unknown error while launch spot instances, %s", err.Error())
			return
		}
		time.Sleep(time.Second * 2)
	}
	clockSkewRoundedUp := roundDurationUp(approxClockSkew)
	c.spot.approximateClockSkew = clockSkewRoundedUp
}

// Convert c.spot.trackedReqs to a list of Instances. For the requests that have
// been fulfilled, this requires querying the EC2 API to find the instance state.
func (c *awsCluster) buildInstanceListFromTrackedReqs(
	ctx *actor.Context,
) ([]*model.Instance, error) {
	runningSpotInstanceIds := newSetOfStrings()
	pendingSpotRequestsAsInstances := make([]*model.Instance, 0)

	for _, activeRequest := range c.spot.trackedReqs.iter() {
		if activeRequest.InstanceID != nil {
			runningSpotInstanceIds.add(*activeRequest.InstanceID)
		} else {
			pendingSpotRequestsAsInstances = append(pendingSpotRequestsAsInstances, &model.Instance{
				ID:         activeRequest.SpotRequestID,
				LaunchTime: activeRequest.CreationTime,
				AgentName:  activeRequest.SpotRequestID,
				State:      model.SpotRequestPendingAWS,
			})
		}
	}

	instancesToReturn, err := c.describeInstancesByID(
		runningSpotInstanceIds.asListOfPointers(),
		false,
	)
	if err != nil {
		return []*model.Instance{}, errors.Wrap(err, "cannot describe EC2 instances")
	}

	// Ignore any instances in the terminated state. The can happen due to eventual consistency (the
	// instance has been terminated, the spot request should be 'closed' with the status
	// 'instance-terminated-by-user', but the spot API still shows the request as 'fulfilled'). If we
	// don't correct for this, the user could have no GPUs actually provisioned, but the output of
	// listSpot is telling the scale decider that there are GPUs available so it won't provision more.
	// Also ignore instances that are shutting down, so future provisioning isn't blocked by the
	// potentially long shutdown process.
	nonTerminalInstances := make([]*ec2.Instance, 0)
	for _, inst := range instancesToReturn {
		if *inst.State.Name != "terminated" && *inst.State.Name != "shutting-down" {
			nonTerminalInstances = append(nonTerminalInstances, inst)
		}
	}

	realInstances := c.newInstances(nonTerminalInstances)
	for _, inst := range realInstances {
		if inst.State == model.Unknown {
			ctx.Log().Errorf("unknown instance state for instance %v", inst.ID)
		}
	}

	combined := realInstances
	combined = append(combined, pendingSpotRequestsAsInstances...)
	ctx.Log().
		WithField("log-type", "listSpot.returnCombinedList").
		Debugf("Returning list of instances: %d EC2 instances and %d dummy spot instances for %d total.",
			len(realInstances), len(pendingSpotRequestsAsInstances), len(combined))
	return combined, nil
}

func roundDurationUp(d time.Duration) time.Duration {
	roundInterval := time.Second * 10
	rounded := d.Round(roundInterval)
	if rounded < d {
		rounded += roundInterval
	}
	return rounded
}

// The AWS API requires a validFrom time that is in the future according to AWS's clock.
// See documentation of the spot struct for more detail. This function attempts
// to create a spot request using the current values for c.spot.approximateClockSkew
// and c.spot.launchTimeOffset. If that fails because AWS says the validFrom time is
// not in the future, we increase c.spot.launchTimeOffset by launchTimeOffsetGrowth.
// This can happen a maximum of 5 times before exiting with an error, to ensure that this
// function doesn't block for too long.
func (c *awsCluster) createSpotInstanceRequestsCorrectingForClockSkew(
	ctx *actor.Context,
	numInstances int,
	dryRun bool,
) (resp *ec2.RequestSpotInstancesOutput, err error) {
	maxRetries := 5
	for numRetries := 0; numRetries <= maxRetries; numRetries++ {
		offset := c.spot.approximateClockSkew + c.spot.launchTimeOffset
		resp, err = c.createSpotInstanceRequest(ctx, numInstances, c.InstanceType, offset, dryRun)
		if err == nil {
			return resp, nil
		}

		if awsErr, ok := err.(awserr.Error); ok {
			ctx.Log().
				Infof("AWS error while launching spot instances, %s, %s",
					awsErr.Code(),
					awsErr.Message())
			if awsErr.Code() == "InvalidTime" {
				c.spot.launchTimeOffset += launchTimeOffsetGrowth
				ctx.Log().Infof("AWS error while launch spot instances - InvalidTime. Increasing "+
					"launchOffset to %s to correct for clock skew",
					c.spot.launchTimeOffset.String())
			}
		} else {
			ctx.Log().Errorf("unknown error while launch spot instances, %s", err.Error())
			return nil, err
		}
	}
	return nil, err
}

func (c *awsCluster) createSpotInstanceRequest(
	ctx *actor.Context,
	numInstances int,
	instanceType provconfig.Ec2InstanceType,
	launchTimeOffset time.Duration,
	dryRun bool,
) (*ec2.RequestSpotInstancesOutput, error) {
	if dryRun {
		ctx.Log().Debug("dry run of createSpotInstanceRequest.")
	}
	idempotencyToken := uuid.New().String()

	validFrom := time.Now().UTC().Add(c.spot.approximateClockSkew).Add(launchTimeOffset)
	spotInput := &ec2.RequestSpotInstancesInput{
		ClientToken:                  aws.String(idempotencyToken),
		DryRun:                       aws.Bool(dryRun),
		InstanceCount:                aws.Int64(int64(numInstances)),
		InstanceInterruptionBehavior: aws.String("terminate"),
		LaunchSpecification: &ec2.RequestSpotLaunchSpecification{
			BlockDeviceMappings: []*ec2.BlockDeviceMapping{
				{
					DeviceName: aws.String("/dev/sda1"),
					Ebs: &ec2.EbsBlockDevice{
						DeleteOnTermination: aws.Bool(true),
						VolumeSize:          aws.Int64(int64(c.RootVolumeSize)),
						VolumeType:          aws.String("gp2"),
					},
				},
			},
			ImageId:      aws.String(c.ImageID),
			InstanceType: aws.String(instanceType.Name()),
			KeyName:      aws.String(c.SSHKeyName),

			UserData: aws.String(base64.StdEncoding.EncodeToString(c.ec2UserData)),
		},
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: aws.String("spot-instances-request"),
				Tags: []*ec2.Tag{
					{
						Key:   aws.String(c.TagKey),
						Value: aws.String(c.TagValue),
					},
					{
						Key:   aws.String("Name"),
						Value: aws.String(c.InstanceName),
					},
					{
						Key:   aws.String("determined-resource-pool"),
						Value: aws.String(c.resourcePool),
					},
					{
						Key:   aws.String("determined-master-address"),
						Value: aws.String(c.masterURL.String()),
					},
				},
			},
		},
		ValidFrom: aws.Time(validFrom),
	}

	// Excluding the SpotPrice param automatically uses the on-demand price
	if c.SpotMaxPrice != provconfig.SpotPriceNotSetPlaceholder {
		spotInput.SpotPrice = aws.String(c.AWSClusterConfig.SpotMaxPrice)
	}

	spotInput.LaunchSpecification.NetworkInterfaces = []*ec2.InstanceNetworkInterfaceSpecification{
		{
			AssociatePublicIpAddress: aws.Bool(c.NetworkInterface.PublicIP),
			DeleteOnTermination:      aws.Bool(true),
			Description:              aws.String("network interface created by Determined"),
			DeviceIndex:              aws.Int64(0),
		},
	}
	if c.NetworkInterface.SubnetID != "" {
		subnet := aws.String(c.NetworkInterface.SubnetID)
		spotInput.LaunchSpecification.NetworkInterfaces[0].SubnetId = subnet
	}
	if c.NetworkInterface.SecurityGroupID != "" {
		spotInput.LaunchSpecification.NetworkInterfaces[0].Groups = []*string{
			aws.String(c.NetworkInterface.SecurityGroupID),
		}
	}

	if c.IamInstanceProfileArn != "" {
		spotInput.LaunchSpecification.IamInstanceProfile = &ec2.IamInstanceProfileSpecification{
			Arn: aws.String(c.IamInstanceProfileArn),
		}
	}

	return c.client.RequestSpotInstances(spotInput)
}

func (c *awsCluster) listCanceledButInstanceRunningSpotRequests(
	ctx *actor.Context,
	dryRun bool,
) (reqs *setOfSpotRequests, err error) {
	if dryRun {
		ctx.Log().Debug("dry run of listCanceledButInstanceRunningSpotInstanceRequests.")
	}

	input := &ec2.DescribeSpotInstanceRequestsInput{
		DryRun: aws.Bool(dryRun),
		Filters: []*ec2.Filter{
			{
				Name: aws.String(fmt.Sprintf("tag:%s", c.TagKey)),
				Values: []*string{
					aws.String(c.TagValue),
				},
			},
			{
				Name:   aws.String(fmt.Sprintf("tag:%s", "determined-resource-pool")),
				Values: []*string{aws.String(c.resourcePool)},
			},
			{
				Name: aws.String("status-code"),
				Values: []*string{
					aws.String("request-canceled-and-instance-running"),
				},
			},
		},
	}

	response, err := c.client.DescribeSpotInstanceRequests(input)
	if err != nil {
		return
	}

	ret := newSetOfSpotRequests()
	for _, req := range response.SpotInstanceRequests {
		ret.add(&spotRequest{
			SpotRequestID: *req.SpotInstanceRequestId,
			State:         *req.State,
			StatusCode:    req.Status.Code,
			StatusMessage: req.Status.Message,
			InstanceID:    req.InstanceId,
			CreationTime:  *req.CreateTime,
		})
	}

	return &ret, nil
}

func (c *awsCluster) listActiveSpotInstanceRequests(
	ctx *actor.Context,
	dryRun bool,
) (reqs *setOfSpotRequests, err error) {
	if dryRun {
		ctx.Log().Debug("dry run of listActiveSpotInstanceRequests.")
	}

	input := &ec2.DescribeSpotInstanceRequestsInput{
		DryRun: aws.Bool(dryRun),
		Filters: []*ec2.Filter{
			{
				Name: aws.String(fmt.Sprintf("tag:%s", c.TagKey)),
				Values: []*string{
					aws.String(c.TagValue),
				},
			},
			{
				Name:   aws.String(fmt.Sprintf("tag:%s", "determined-resource-pool")),
				Values: []*string{aws.String(c.resourcePool)},
			},
			{
				Name: aws.String("state"),
				Values: []*string{
					aws.String("open"),
					aws.String("active"),
				},
			},
		},
	}

	response, err := c.client.DescribeSpotInstanceRequests(input)
	if err != nil {
		return nil, err
	}

	ret := newSetOfSpotRequests()
	for _, req := range response.SpotInstanceRequests {
		ret.add(&spotRequest{
			SpotRequestID: *req.SpotInstanceRequestId,
			State:         *req.State,
			StatusCode:    req.Status.Code,
			StatusMessage: req.Status.Message,
			InstanceID:    req.InstanceId,
			CreationTime:  *req.CreateTime,
		})
	}

	return &ret, nil
}

// List all spot requests that match a list of spot request ids. We use a filter instead
// of the SpotInstanceRequestIds param= because the spotRequestIds in the input may not
// yet exist in the AWS API (due to eventual consistency) and we don't want the API call
// to fail - we want it to return successfully, just excluding those requests.
func (c *awsCluster) listSpotRequestsByID(
	ctx *actor.Context,
	spotRequestIds []*string,
	dryRun bool,
) (*setOfSpotRequests, error) {
	if dryRun {
		ctx.Log().Debug("dry run of listSpotRequestsByID.")
	}

	if len(spotRequestIds) == 0 {
		emptyResponse := newSetOfSpotRequests()
		return &emptyResponse, nil
	}

	input := &ec2.DescribeSpotInstanceRequestsInput{
		DryRun: aws.Bool(dryRun),
		Filters: []*ec2.Filter{
			{
				Name: aws.String(fmt.Sprintf("tag:%s", c.TagKey)),
				Values: []*string{
					aws.String(c.TagValue),
				},
			},
			{
				Name:   aws.String(fmt.Sprintf("tag:%s", "determined-resource-pool")),
				Values: []*string{aws.String(c.resourcePool)},
			},
			{
				Name:   aws.String("spot-instance-request-id"),
				Values: spotRequestIds,
			},
		},
	}

	response, err := c.client.DescribeSpotInstanceRequests(input)
	if err != nil {
		return nil, err
	}

	ret := newSetOfSpotRequests()
	for _, req := range response.SpotInstanceRequests {
		ret.add(&spotRequest{
			SpotRequestID: *req.SpotInstanceRequestId,
			State:         *req.State,
			StatusCode:    req.Status.Code,
			StatusMessage: req.Status.Message,
			InstanceID:    req.InstanceId,
			CreationTime:  *req.CreateTime,
		})
	}

	return &ret, nil
}

func (c *awsCluster) terminateSpotInstanceRequests(
	ctx *actor.Context,
	spotRequestIds []*string,
	dryRun bool,
) (*ec2.CancelSpotInstanceRequestsOutput, error) {
	if len(spotRequestIds) == 0 {
		return &ec2.CancelSpotInstanceRequestsOutput{}, nil
	}
	input := &ec2.CancelSpotInstanceRequestsInput{
		DryRun:                 aws.Bool(dryRun),
		SpotInstanceRequestIds: spotRequestIds,
	}

	return c.client.CancelSpotInstanceRequests(input)
}
