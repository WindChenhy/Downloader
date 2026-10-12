import type {SpeedPoint} from '../types';
import {t} from '../i18n';
import {formatSpeed} from '../lib/format';
import {
  CHART_BASELINE,
  CHART_BOX,
  buildSeries,
  clockLabel,
  decimate,
  tickValues,
  yForValue,
} from '../lib/stats';

interface Props {
  points: SpeedPoint[];
  /** 横轴范围（毫秒时间戳）：采样只在有速度时记点，空洞必须按真实时间留白 */
  t0: number;
  t1: number;
  /** 纵轴上界，多个图共用同一刻度时可直接比较 */
  vmax: number;
  caption?: string;
}

// 上千点画进 SVG 会拖慢重绘，而 640 宽的图里根本看不出差别
const MAX_POINTS = 300;

/** SpeedChart 手写 SVG 速度曲线：面积 + 折线 + 网格刻度，不引图表依赖。 */
export default function SpeedChart({points, t0, t1, vmax, caption}: Props) {
  const shown = decimate(points ?? [], MAX_POINTS);
  const series = buildSeries(shown, t0, t1, vmax);
  // vmax 很小时四等分会算出重复刻度（三个 0 叠在基线上），去重后只画一条
  const ticks = Array.from(new Set(tickValues(vmax, 4)));
  const box = CHART_BOX;

  return (
    <div className="chart">
      {caption && <div className="chart-caption">{caption}</div>}
      <svg
        className="chart-svg"
        viewBox={`0 0 ${box.w} ${box.h}`}
        role="img"
        aria-label={t('速度曲线')}
        preserveAspectRatio="xMidYMid meet"
      >
        {ticks.map((v) => {
          const y = yForValue(v, vmax);
          return (
            <g key={v}>
              <line className="chart-grid" x1={box.padL} y1={y} x2={box.w - box.padR} y2={y} />
              <text className="chart-axis" x={box.padL - 6} y={y + 3} textAnchor="end">
                {v > 0 ? formatSpeed(v) : '0'}
              </text>
            </g>
          );
        })}
        {series.area && <path className="chart-area" d={series.area} />}
        {series.line && <path className="chart-line" d={series.line} />}
        {series.last && <circle className="chart-dot" cx={series.last.x} cy={series.last.y} r={3} />}
        <line
          className="chart-grid solid"
          x1={box.padL}
          y1={CHART_BASELINE}
          x2={box.w - box.padR}
          y2={CHART_BASELINE}
        />
        <text className="chart-axis" x={box.padL} y={box.h - 6} textAnchor="start">
          {clockLabel(t0)}
        </text>
        <text className="chart-axis" x={box.w - box.padR} y={box.h - 6} textAnchor="end">
          {clockLabel(t1)}
        </text>
        {!series.line && (
          <text className="chart-empty" x={box.w / 2} y={box.h / 2} textAnchor="middle">
            {t('暂无速度采样')}
          </text>
        )}
      </svg>
    </div>
  );
}
