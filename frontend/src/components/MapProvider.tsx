import { useEffect, type ReactNode } from 'react'
import { Alert, Button, Flex, Skeleton } from 'antd'
import { useSchoolMapsKey } from '../hooks/useSchoolMapsKey'

// The Google Maps script can be loaded only once per page, with one key. If a
// Super Admin switches to a school with a different key, a reload is needed.
const googleScript: { key: string | null } = { key: null }

/**
 * Chooses the map provider for the current school: `google(key)` when the school has
 * a Google Maps browser key (Settings → Maps), otherwise `osm()` (OpenStreetMap).
 */
export default function MapProvider({
  height,
  google,
  osm,
}: {
  height: number
  google: (apiKey: string) => ReactNode
  osm: () => ReactNode
}) {
  const key = useSchoolMapsKey()

  useEffect(() => {
    if (key) googleScript.key ??= key
  }, [key])

  if (key === undefined) return <Skeleton.Node active style={{ width: '100%', height }} />
  if (!key) return <>{osm()}</>
  if (googleScript.key && googleScript.key !== key) {
    return (
      <Flex vertical gap="small">
        <Alert
          type="info"
          showIcon
          title="This school uses a different Google Maps key."
          action={
            <Button size="small" onClick={() => window.location.reload()}>
              Reload
            </Button>
          }
        />
        {osm()}
      </Flex>
    )
  }
  return <>{google(key)}</>
}
