import { useEffect, type ReactNode } from 'react'
import { Alert, Button, Flex, Skeleton } from 'antd'
import { useSchoolMapsKey } from '../hooks/useSchoolMapsKey'
import MapsConfigError from './MapsConfigError'

// The Google Maps script can be loaded only once per page, with one key. If a
// Super Admin switches to a school with a different key, a reload is needed.
const googleScript: { key: string | null } = { key: null }

/**
 * Chooses the map provider for the current school: `google(key)` when the school has
 * a Google Maps browser key (Settings → Maps). Google Maps is the only provider —
 * a school with no key gets a clear configuration message, never a silent fallback
 * to another map service.
 */
export default function MapProvider({
  height,
  google,
}: {
  height: number
  google: (apiKey: string) => ReactNode
}) {
  const key = useSchoolMapsKey()

  useEffect(() => {
    if (key) googleScript.key ??= key
  }, [key])

  if (key === undefined) return <Skeleton.Node active style={{ width: '100%', height }} />
  if (!key) return <MapsConfigError height={height} />
  if (googleScript.key && googleScript.key !== key) {
    return (
      <Flex
        vertical
        align="center"
        justify="center"
        gap="small"
        style={{ height, border: '1px dashed #d9d9d9', borderRadius: 8, padding: 24 }}
      >
        <Alert
          type="info"
          showIcon
          title="This school uses a different Google Maps key. Reload to switch."
          action={
            <Button size="small" onClick={() => window.location.reload()}>
              Reload
            </Button>
          }
        />
      </Flex>
    )
  }
  return <>{google(key)}</>
}
