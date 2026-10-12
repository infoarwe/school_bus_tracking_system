import { useEffect, useRef, useState } from 'react'
import { APIProvider, useMapsLibrary } from '@vis.gl/react-google-maps'
import { Alert, AutoComplete, Flex, Input, Spin, Typography } from 'antd'
import { EnvironmentOutlined, SearchOutlined } from '@ant-design/icons'
import { useSchoolMapsKey } from '../hooks/useSchoolMapsKey'
import MapsConfigError from './MapsConfigError'

/** A place the user picked: a readable name/address and its position. */
export interface PlaceResult {
  name: string
  address: string
  latitude: number
  longitude: number
}

interface Props {
  onSelect: (place: PlaceResult) => void
  /** Bias results towards this point (e.g. the route's other stops). */
  near?: { lat: number; lng: number } | null
  placeholder?: string
  autoFocus?: boolean
}

/**
 * Place search for stops and routes. Google Places Autocomplete (New) only, with the
 * school's own key (Settings → Maps) — suggestions appear while typing. A school with
 * no key gets a clear configuration message, never a silent fallback to another
 * geocoding service.
 */
export default function LocationSearch(props: Props) {
  const key = useSchoolMapsKey()
  if (key === undefined) return <Input disabled placeholder="Loading place search…" />
  if (!key) return <MapsConfigError compact />
  return (
    <APIProvider apiKey={key}>
      <GoogleSearch {...props} />
    </APIProvider>
  )
}

const DEFAULT_PLACEHOLDER = 'Search a place, street or landmark'

// --- Google Places Autocomplete (New) ------------------------------------------

interface Suggestion {
  id: string
  main: string
  secondary: string
  prediction: google.maps.places.PlacePrediction
}

function GoogleSearch({ onSelect, near, placeholder, autoFocus }: Props) {
  const places = useMapsLibrary('places')
  const [text, setText] = useState('')
  const [suggestions, setSuggestions] = useState<Suggestion[]>([])
  const [loading, setLoading] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  // One billing session per search: from the first keystroke to the selected place.
  const session = useRef<google.maps.places.AutocompleteSessionToken | null>(null)
  const seq = useRef(0)
  const query = text.trim()
  // The Places library loads asynchronously; a query typed before it is ready runs
  // as soon as it is (this effect re-runs when `places` arrives).
  const ready = !!places
  const nearLat = near?.lat
  const nearLng = near?.lng

  // The text of a chosen place: it is not searched again (that would start a new billing session).
  const [chosen, setChosen] = useState<string | null>(null)

  useEffect(() => {
    if (!places || query.length < 3 || query === chosen) return
    const mine = ++seq.current
    const timer = window.setTimeout(async () => {
      session.current ??= new places.AutocompleteSessionToken()
      setLoading(true)
      try {
        const { suggestions: found } =
          await places.AutocompleteSuggestion.fetchAutocompleteSuggestions({
            input: query,
            sessionToken: session.current,
            ...(nearLat != null && nearLng != null
              ? { locationBias: { center: { lat: nearLat, lng: nearLng }, radius: 30000 } }
              : {}),
          })
        if (mine !== seq.current) return // a newer search is running
        setProblem(null)
        setSuggestions(
          found.flatMap((s) =>
            s.placePrediction
              ? [
                  {
                    id: s.placePrediction.placeId,
                    main: s.placePrediction.mainText?.text ?? s.placePrediction.text.text,
                    secondary: s.placePrediction.secondaryText?.text ?? '',
                    prediction: s.placePrediction,
                  },
                ]
              : [],
          ),
        )
      } catch {
        if (mine !== seq.current) return
        setSuggestions([])
        // Usually: Places API (New) is not enabled for this key, or the key is restricted.
        setProblem(
          "Place search did not work with this school's Google Maps key. A School Admin can enable " +
            '"Places API (New)" for the key in Google Cloud. Meanwhile, click the map to place it.',
        )
      } finally {
        if (mine === seq.current) setLoading(false)
      }
    }, 300)
    return () => window.clearTimeout(timer)
  }, [places, query, chosen, nearLat, nearLng])

  const shown = query.length >= 3 ? suggestions : []

  async function pick(id: string) {
    const s = suggestions.find((x) => x.id === id)
    if (!s) return
    setLoading(true)
    try {
      const place = s.prediction.toPlace()
      await place.fetchFields({ fields: ['displayName', 'formattedAddress', 'location'] })
      session.current = null // the session ends with this place
      if (!place.location) throw new Error('no location')
      onSelect({
        name: place.displayName ?? s.main,
        address: place.formattedAddress ?? [s.main, s.secondary].filter(Boolean).join(', '),
        latitude: Number(place.location.lat().toFixed(6)),
        longitude: Number(place.location.lng().toFixed(6)),
      })
      setChosen(s.main.trim())
      setText(s.main)
      setSuggestions([])
    } catch {
      setProblem('Could not load that place. Try another result, or click the map.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Flex vertical gap={6}>
      <AutoComplete
        value={text}
        options={shown.map((s) => ({
          value: s.id,
          label: (
            <Flex gap={8} align="baseline">
              <EnvironmentOutlined style={{ color: '#64748b' }} />
              <span style={{ minWidth: 0 }}>
                <Typography.Text strong>{s.main}</Typography.Text>
                {s.secondary && (
                  <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    {s.secondary}
                  </Typography.Text>
                )}
              </span>
            </Flex>
          ),
        }))}
        onSearch={(v) => {
          setChosen(null)
          setText(v)
        }}
        onSelect={(id: string) => void pick(id)}
        notFoundContent={
          !ready && query.length >= 3 ? (
            'Loading place search…'
          ) : loading ? (
            <Spin size="small" />
          ) : query.length >= 3 && !problem && query !== chosen ? (
            'No places found. Try a nearby landmark, or click the map.'
          ) : null
        }
        style={{ width: '100%' }}
      >
        <Input
          size="large"
          prefix={<SearchOutlined />}
          suffix={loading ? <Spin size="small" /> : null}
          placeholder={placeholder ?? DEFAULT_PLACEHOLDER}
          aria-label="Search for a place"
          autoFocus={autoFocus}
          allowClear
        />
      </AutoComplete>
      {problem ? (
        <Alert type="warning" showIcon title={problem} />
      ) : (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          Type at least 3 letters, then choose a suggestion (Google Maps).
        </Typography.Text>
      )}
    </Flex>
  )
}
