export interface Activity {
  ID: number
  Name: string
  Color: string
  ProjectID: number
  Archived: boolean
}

export interface Project {
  ID: number
  Slug: string
  Name: string
  Color: string
  Archived: boolean
  Billable: boolean
}

export interface Tag {
  ID: number
  Name: string
}

export interface Session {
  ID: number
  ActivityID: number
  ActivityName: string
  Color: string
  ProjectID: number
  ProjectName: string
  ProjectColor: string
  ProjectSlug: string
  StartISO: string
  ResumeISO: string
  Clock: string
  StartLocal: string
  EndLocal: string
  Duration: string
  DurationSecs: number
  DurationInput: string
  AccumulatedSeconds: number
  Paused: boolean
  Note: string
  Tags: Tag[]
}

export interface Goal {
  ID: number
  ActivityName: string
  Color: string
  TargetLabel: string
  AchievedLabel: string
  Percent: number
  PeriodRangeLabel: string
}

export interface UnbilledItem {
  ProjectID: number
  ProjectName: string
  Slug: string
  Hours: string
  Amount: string
  Since: string
  SinceISO: string
}

export interface DashboardData {
  Title: string
  Active: string
  User?: { ID: number; Email: string; Name: string } | null
  Team?: { ID: number; Name: string } | null
  UserTeams: Array<{ ID: number; Name: string; Role: string }>
  RequestPath: string
  CSRFToken: string
  Lang: string
  CanManage: boolean
  Mods: Record<string, boolean>
  Widgets: Record<string, boolean>
  ReactApp: boolean
  Activities: Activity[]
  Projects: Project[]
  ActiveSessions: Session[]
  Recent: Session[]
  ActiveCount: number
  RunningCount: number
  PausedCount: number
  TodayTotal: string
  TodaySecs: number
  TopToday: string
  Goals: Goal[]
  HasProject: boolean
  HasSession: boolean
  Unbilled: UnbilledItem[]
  DefaultProject: number
}

export interface ProjectListRow {
  ID: number
  Slug: string
  Name: string
  Color: string
  Archived: boolean
  Activities: number
  TodaySecs: number
  MonthSecs: number
  TodayLabel: string
  MonthLabel: string
}

export interface ProjectListData {
  Active: string
  Lang: string
  Projects: ProjectListRow[]
  ShowArchived: boolean
  Flash: string
  FlashOK: boolean
}

export interface ProjectDetailData {
  Title: string
  Active: string
  ReactApp: boolean
  Lang: string
  Project: Project
  Activities: Activity[]
  Sessions: Session[]
  Total: string
  MonthTotal: string
  Archived: boolean
  EstimateLabel: string
  EstimateInput: string
  EstimatePercent: number
  RateInput: string
  Currency: string
  TeamCurrency: string
  Currencies: Array<{ Code: string; Label: string }>
  Flash: string
  FlashOK: boolean
  CSRFToken: string
  CanManage: boolean
  Unbilled: UnbilledItem[]
}

export interface ProjectCreateData {
  Title: string
  Active: string
  ReactApp: boolean
  NewProject: boolean
  Lang: string
  CSRFToken: string
  Currencies: Array<{ Code: string; Label: string }>
  TeamCurrency: string
}

export type ReactPageData = DashboardData | ProjectListData | ProjectDetailData | ProjectCreateData

export interface ReactPageBootstrap {
	data: ReactPageData
}
