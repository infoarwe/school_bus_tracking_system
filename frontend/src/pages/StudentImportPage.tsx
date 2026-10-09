import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Alert, App, Button, Card, Flex, Statistic, Table, Typography, Upload } from 'antd'
import { ArrowLeftOutlined, DownloadOutlined, InboxOutlined } from '@ant-design/icons'
import PageHeader from '../components/PageHeader'
import RequireSchool from '../components/RequireSchool'
import { studentsApi } from '../services/people'
import type { ImportResult } from '../services/types'
import { errorMessage } from '../utils/formErrors'

// Matches ImportColumns in backend/internal/handlers/import.go.
const TEMPLATE =
  'admission_no,student_name,class,section,parent_name,parent_mobile,parent_relationship,parent2_name,parent2_mobile,parent2_relationship,route_code,pickup_stop,drop_stop\n' +
  'DPS-2001,Asha Ravi,5,A,Ravi Kumar,9876543210,father,Meena Ravi,9876543211,mother,RS-01,Peelamedu,Peelamedu\n' +
  'DPS-2002,Arjun Ravi,2,B,Ravi Kumar,9876543210,father,,,,RS-01,2,\n'

function downloadTemplate() {
  const url = URL.createObjectURL(new Blob([TEMPLATE], { type: 'text/csv' }))
  const a = document.createElement('a')
  a.href = url
  a.download = 'students-import-template.csv'
  a.click()
  URL.revokeObjectURL(url)
}

export default function StudentImportPage() {
  return <RequireSchool>{(schoolId) => <StudentImport schoolId={schoolId} />}</RequireSchool>
}

function StudentImport({ schoolId }: { schoolId: string }) {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const [file, setFile] = useState<File | null>(null)
  const [result, setResult] = useState<ImportResult | null>(null)
  const [busy, setBusy] = useState<'check' | 'import' | null>(null)

  async function run(dryRun: boolean) {
    if (!file) return
    setBusy(dryRun ? 'check' : 'import')
    try {
      const res = await studentsApi.import(schoolId, file, dryRun)
      setResult(res)
      if (res.imported) message.success('Import complete.')
    } catch (e) {
      setResult(null)
      message.error(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  const checkedClean = result?.dry_run && result.errors.length === 0

  return (
    <Flex vertical gap="middle">
      <Button
        type="link"
        icon={<ArrowLeftOutlined />}
        onClick={() => navigate('/students')}
        style={{ alignSelf: 'flex-start', padding: 0 }}
      >
        Students
      </Button>
      <PageHeader
        title="Import students"
        subtitle="Add or update students, link parents and set routes from a CSV file."
        extra={
          <Button icon={<DownloadOutlined />} onClick={downloadTemplate}>
            Download template
          </Button>
        }
      />
      <Card>
        <Typography.Paragraph>
          One row per student. <code>admission_no</code> and <code>student_name</code> are required.
          Existing students (same admission number) are updated. Parents are matched by mobile, so
          siblings can share a parent. Stops can be given by name or stop number. If only one stop
          is given, it is used for both pickup and drop.
        </Typography.Paragraph>
        <Typography.Paragraph type="secondary">
          Nothing is saved if any row has an error. Check the file first, fix the rows listed (each
          row shows its first problem), check again, then import.
        </Typography.Paragraph>
        <Upload.Dragger
          accept=".csv,text/csv"
          maxCount={1}
          beforeUpload={(f) => {
            setFile(f)
            setResult(null)
            return false // keep the file in the browser; we upload it ourselves
          }}
          onRemove={() => {
            setFile(null)
            setResult(null)
          }}
        >
          <p className="ant-upload-drag-icon">
            <InboxOutlined />
          </p>
          <p className="ant-upload-text">Click or drop a CSV file here (max 2,000 rows)</p>
        </Upload.Dragger>
        <Flex gap="small" style={{ marginTop: 16 }}>
          <Button disabled={!file} loading={busy === 'check'} onClick={() => void run(true)}>
            Check file
          </Button>
          <Button
            type="primary"
            disabled={!checkedClean}
            loading={busy === 'import'}
            onClick={() => void run(false)}
          >
            Import
          </Button>
        </Flex>
      </Card>

      {result && (
        <Card
          title={
            result.imported
              ? 'Imported'
              : result.errors.length
                ? `${result.errors.length} problem(s) found: nothing was saved`
                : 'File checked: ready to import'
          }
        >
          {result.errors.length === 0 && (
            <Alert
              type={result.imported ? 'success' : 'info'}
              showIcon
              style={{ marginBottom: 16 }}
              title={
                result.imported
                  ? 'All rows were saved.'
                  : 'No problems found. Press Import to save these changes.'
              }
            />
          )}
          <Flex gap="large" wrap style={{ marginBottom: 16 }}>
            <Statistic title="Rows" value={result.total_rows} />
            <Statistic title="New students" value={result.students_created} />
            <Statistic title="Updated students" value={result.students_updated} />
            <Statistic title="New parents" value={result.parents_created} />
            <Statistic title="Parent links" value={result.parent_links} />
            <Statistic title="Route assignments" value={result.assignments_set} />
          </Flex>
          {result.errors.length > 0 && (
            <Table
              rowKey={(e) => `${e.row}-${e.field}`}
              size="small"
              dataSource={[...result.errors].sort((a, b) => a.row - b.row)}
              pagination={{ pageSize: 50, hideOnSinglePage: true }}
              columns={[
                { title: 'Line', dataIndex: 'row', width: 80 },
                { title: 'Column', dataIndex: 'field', width: 180 },
                { title: 'Problem', dataIndex: 'message' },
              ]}
            />
          )}
        </Card>
      )}
    </Flex>
  )
}
