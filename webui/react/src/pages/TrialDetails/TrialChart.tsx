import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { AlignedData } from 'uplot';

import Empty from 'components/kit/Empty';
import MetricSelect from 'components/MetricSelect';
import ResponsiveFilters from 'components/ResponsiveFilters';
import ScaleSelect from 'components/ScaleSelect';
import Section from 'components/Section';
import Spinner from 'components/Spinner';
import UPlotChart, { Options } from 'components/UPlot/UPlotChart';
import { tooltipsPlugin } from 'components/UPlot/UPlotChart/tooltipsPlugin';
import { trackAxis } from 'components/UPlot/UPlotChart/trackAxis';
import usePolling from 'hooks/usePolling';
import css from 'pages/TrialDetails/TrialChart.module.scss';
import { timeSeries } from 'services/api';
import { Metric, MetricContainer, Scale } from 'types';
import { glasbeyColor } from 'utils/color';

interface Props {
  defaultMetricNames: Metric[];
  id?: string;
  metricNames: Metric[];
  metrics: Metric[];
  onMetricChange: (value: Metric[]) => void;
  trialId?: number;
  trialTerminated: boolean;
}

const getChartMetricLabel = (metric: Metric): string => {
  if (metric.type === 'training') return `[T] ${metric.name}`;
  if (metric.type === 'validation') return `[V] ${metric.name}`;
  return metric.name;
};

const TrialChart: React.FC<Props> = ({
  defaultMetricNames,
  metricNames,
  metrics,
  onMetricChange,
  trialId,
  trialTerminated,
}: Props) => {
  const [scale, setScale] = useState<Scale>(Scale.Linear);
  const [trialSumm, setTrialSummary] = useState<MetricContainer[]>([]);

  const fetchTrialSummary = useCallback(async () => {
    if (trialId) {
      const summ = await timeSeries({
        maxDatapoints: screen.width > 1600 ? 1500 : 1000,
        metricNames: metricNames,
        scale: scale,
        startBatches: 0,
        trialIds: [trialId],
      });
      setTrialSummary(summ[0].metrics);
    }
  }, [metricNames, scale, trialId]);

  const { stopPolling } = usePolling(fetchTrialSummary, { interval: 2000, rerunOnNewFn: true });

  useEffect(() => {
    if (trialTerminated) {
      stopPolling();
    }
  }, [trialTerminated, stopPolling]);

  if (trialTerminated) {
    stopPolling();
  }

  const chartData: AlignedData = useMemo(() => {
    const xValues: number[] = [];
    const yValues: Record<string, Record<string, number | null>> = {};

    metrics.forEach((metric, index) => {
      yValues[index] = {};

      const mWrapper = trialSumm.find((mContainer) => mContainer.type === metric.type);
      if (!mWrapper?.data) {
        return;
      }

      mWrapper.data.forEach((avgMetrics) => {
        if (avgMetrics.values[metric.name] || avgMetrics.values[metric.name] === 0) {
          if (!xValues.includes(avgMetrics.batches)) {
            xValues.push(avgMetrics.batches);
          }
          yValues[index][avgMetrics.batches] = Number.isFinite(avgMetrics.values[metric.name])
            ? avgMetrics.values[metric.name]
            : null;
        }
      });
    });

    xValues.sort((a, b) => a - b);

    const yValuesArray: (number | null)[][] = Object.values(yValues).map((yValue) => {
      return xValues.map((xValue) => (yValue[xValue] != null ? yValue[xValue] : null));
    });

    return [xValues, ...yValuesArray];
  }, [metrics, trialSumm]);

  const chartOptions: Options = useMemo(() => {
    return {
      axes: [
        { label: 'Batches' },
        { label: metrics.length === 1 ? getChartMetricLabel(metrics[0]) : 'Metric Value' },
      ],
      height: 400,
      key: trialId,
      legend: { show: false },
      plugins: [
        tooltipsPlugin({ closeOnMouseExit: true, isShownEmptyVal: true, seriesColors: [] }),
        trackAxis(),
      ],
      scales: { x: { time: false }, y: { distr: scale === Scale.Log ? 3 : 1 } },
      series: [
        { label: 'Batch' },
        ...metrics.map((metric, index) => ({
          label: getChartMetricLabel(metric),
          spanGaps: true,
          stroke: glasbeyColor(index),
          width: 2,
        })),
      ],
    };
  }, [metrics, scale, trialId]);

  const options = (
    <ResponsiveFilters>
      <MetricSelect
        defaultMetrics={defaultMetricNames}
        metrics={metricNames}
        multiple
        value={metrics}
        onChange={onMetricChange}
      />
      <ScaleSelect value={scale} onChange={setScale} />
    </ResponsiveFilters>
  );

  return (
    <Section bodyBorder options={options} title="Metrics">
      <div className={css.base}>
        {
          <Spinner className={css.spinner} conditionalRender spinning={!trialId}>
            {chartData[0].length === 0 ? (
              <Empty description="No data to plot." icon="error" />
            ) : (
              <UPlotChart data={chartData} options={chartOptions} />
            )}
          </Spinner>
        }
      </div>
    </Section>
  );
};

export default TrialChart;
