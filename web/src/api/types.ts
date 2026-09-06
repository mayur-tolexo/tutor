// Wire types for the /v1 API. Field names match the JSON exactly.

export type Lang = 'hinglish' | 'en'

export type ErrorCode =
  | 'invalid'
  | 'unauthorized'
  | 'not_found'
  | 'rate_limited'
  | 'pool_busy'
  | 'infra_error'

export interface ApiErrorBody {
  error: { code: ErrorCode | string; message: string; retry_after_seconds?: number }
}

export interface Config {
  google_client_id: string
}

export interface TrackExercise {
  id: string
  title: string
  difficulty: 1 | 2 | 3
  must_pass: boolean
}

export interface TrackUnit {
  id: string
  title: string
  exercises: TrackExercise[]
}

export interface Track {
  id: string
  title: string
  units: TrackUnit[]
}

export interface TracksResponse {
  tracks: Track[]
}

export interface VisibleCase {
  id: string
  stdin: string
  stdout: string
}

export interface Exercise {
  id: string
  title: string
  statement_md: string
  starter: string
  concepts: string[]
  syllabus: string
  difficulty: number
  visible_cases: VisibleCase[]
}

export type AttemptMode = 'run' | 'submit'

export interface AttemptRequest {
  exercise_id: string
  code: string
  mode: AttemptMode
  stdin?: string
  idempotency_key: string
}

export type Outcome =
  | 'passed'
  | 'failed'
  | 'runtime_error'
  | 'syntax_error'
  | 'timeout'
  | 'infra_error'

export interface RunResult {
  stdout: string
  stderr: string
  exit_code: number
  timed_out: boolean
}

export interface CaseResult {
  id: string
  passed: boolean
  hidden: boolean
  timed_out: boolean
  stdin?: string
  expected?: string
  actual?: string
  stderr?: string
}

export interface Attempt {
  attempt_id: string
  outcome: Outcome
  passed: boolean
  duration_ms: number
  run?: RunResult
  cases?: CaseResult[]
  error?: { type: string; message: string; line?: number }
}

export interface HintRequest {
  attempt_id: string
  question?: string
}

export interface Hint {
  hint_id: string
  level: 1 | 2 | 3
  source: 'canned' | 'cache' | 'model' | 'degraded'
  lang: Lang
  text: string
  line?: number
}

export type ProgressStatus = 'attempted' | 'passed'

export interface Progress {
  exercises: Record<string, { status: ProgressStatus; passed_at?: string }>
}

export interface Me {
  signed_in: boolean
  display_name?: string
  lang_pref: Lang
  device_id: string
}
