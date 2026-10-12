import type { DetailedHTMLProps, HTMLAttributes, Ref } from 'react'

// <model-viewer> is a custom element registered by @google/model-viewer. Only the attributes the
// Studio's model viewer sets are declared.
declare module 'react' {
  namespace JSX {
    interface IntrinsicElements {
      'model-viewer': DetailedHTMLProps<HTMLAttributes<HTMLElement>, HTMLElement> & {
        ref?: Ref<HTMLElement>
        src?: string
        alt?: string
        'camera-controls'?: string
        'auto-rotate'?: string
        'environment-image'?: string
        'shadow-intensity'?: string
        'touch-action'?: string
      }
    }
  }
}
