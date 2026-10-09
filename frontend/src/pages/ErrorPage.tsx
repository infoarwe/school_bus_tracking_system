import { useRouteError } from 'react-router-dom'
import { Button, Flex, Result, Typography } from 'antd'

/** Shown instead of React Router's developer screen when a page crashes. */
export default function ErrorPage() {
  const error = useRouteError()
  const detail = error instanceof Error ? error.message : String(error)
  return (
    <Flex align="center" justify="center" style={{ minHeight: '80vh', padding: 16 }}>
      <Result
        status="error"
        title="Something went wrong on this page"
        subTitle="Reload to try again. If it keeps happening, send this message to the support team."
        extra={[
          <Button key="reload" type="primary" onClick={() => window.location.reload()}>
            Reload
          </Button>,
          <Button key="home" onClick={() => window.location.assign('/')}>
            Back to dashboard
          </Button>,
        ]}
      >
        <Typography.Text type="secondary" code>
          {detail}
        </Typography.Text>
      </Result>
    </Flex>
  )
}
