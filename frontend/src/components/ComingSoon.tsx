import { Result } from 'antd'

export default function ComingSoon({ title, sprint }: { title: string; sprint: number }) {
  return <Result status="info" title={title} subTitle={`Planned for Sprint ${sprint}.`} />
}
