import { useState } from 'react'
import { Select, Toggle } from '@/design/primitives'
import { hexPoints } from '@/features/graph/layout'
import { useStudio } from '@/state/store'
import type {
  CompactFields,
  DetailedFields,
  HexFields,
  HexShape,
  NodeAppearanceSettings,
  NodeStyle,
  SettingsPatch,
} from '@shared/ipc'
import { Row } from './SettingsView'

const HEX_SIDES: Record<Exclude<HexShape, 'circle'>, number> = { hexagon: 6, octagon: 8 }

const DETAILED_FIELD_LABELS: Record<keyof DetailedFields, string> = {
  modelChip: 'Model chip',
  nodeId: 'Node id',
  artifactCount: 'Artifact count',
  retryCount: 'Retry count',
  duration: 'Duration',
}
const COMPACT_FIELD_LABELS: Record<keyof CompactFields, string> = {
  duration: 'Duration',
  icons: 'Retry / mock icons',
}
const HEX_FIELD_LABELS: Record<keyof HexFields, string> = {
  label: 'Label beneath shape',
  durationOnHover: 'Duration in hover tooltip',
}

export function NodeAppearanceSection({
  patch,
}: {
  patch: (next: SettingsPatch) => Promise<void>
}) {
  const nodeAppearance = useStudio((s) => s.settings!.nodeAppearance)
  const [style, setStyle] = useState<NodeStyle>('detailed')

  async function setScale(scale: number) {
    await patch({
      nodeAppearance: { [style]: { ...nodeAppearance[style], scale } } as Partial<NodeAppearanceSettings>,
    })
  }

  async function setField(key: string, value: boolean) {
    await patch({
      nodeAppearance: {
        [style]: {
          ...nodeAppearance[style],
          fields: { ...nodeAppearance[style].fields, [key]: value },
        },
      } as Partial<NodeAppearanceSettings>,
    })
  }

  const current = nodeAppearance[style]
  const fieldLabels =
    style === 'detailed'
      ? DETAILED_FIELD_LABELS
      : style === 'compact'
        ? COMPACT_FIELD_LABELS
        : HEX_FIELD_LABELS

  return (
    <div className="flex flex-col">
      <Row label="Editing style" description="Each node map style has its own size and field visibility.">
        <Select
          value={style}
          onChange={(event) => setStyle(event.target.value as NodeStyle)}
          className="w-40"
        >
          <option value="detailed">Detailed</option>
          <option value="compact">Compact</option>
          <option value="hex">Hexagonal</option>
        </Select>
      </Row>

      <Row
        label="Size"
        description="Scales the node uniformly — width, height and spacing together — so proportions never distort. Status colour and shape are unaffected."
      >
        <div className="flex items-center gap-2">
          <input
            type="range"
            min={0.85}
            max={1.35}
            step={0.05}
            value={current.scale}
            onChange={(event) => setScale(Number(event.target.value))}
            className="w-40"
          />
          <span className="num w-10 text-right text-2xs text-fg-3">
            {Math.round(current.scale * 100)}%
          </span>
        </div>
      </Row>

      {style === 'hex' ? (
        <Row
          label="Hexagon-style shape"
          description="The outline drawn for this style. Status colour, size and label placement are unaffected."
        >
          <div className="flex items-center gap-3">
            <ShapePreview shape={nodeAppearance.hexShape} />
            <Select
              value={nodeAppearance.hexShape}
              onChange={(event) =>
                patch({ nodeAppearance: { hexShape: event.target.value as HexShape } })
              }
              className="w-36"
            >
              <option value="hexagon">Hexagon</option>
              <option value="octagon">Octagon</option>
              <option value="circle">Circle</option>
            </Select>
          </div>
        </Row>
      ) : null}

      {(Object.keys(fieldLabels) as (keyof typeof fieldLabels)[]).map((key) => (
        <Row key={key} label={fieldLabels[key]}>
          <Toggle
            label={fieldLabels[key]}
            checked={Boolean((current.fields as unknown as Record<string, boolean>)[key as string])}
            onChange={(value) => setField(key as string, value)}
          />
        </Row>
      ))}
    </div>
  )
}

function ShapePreview({ shape }: { shape: HexShape }) {
  const size = 30
  const cx = size / 2
  const cy = size / 2
  const r = size / 2 - 3

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden>
      {shape === 'circle' ? (
        <circle
          cx={cx}
          cy={cy}
          r={r}
          fill="var(--color-inset)"
          stroke="var(--color-st-running)"
          strokeWidth={1.5}
        />
      ) : (
        <polygon
          points={hexPoints(cx, cy, r, HEX_SIDES[shape])}
          fill="var(--color-inset)"
          stroke="var(--color-st-running)"
          strokeWidth={1.5}
          strokeLinejoin="round"
        />
      )}
    </svg>
  )
}
