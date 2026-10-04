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

export interface GoalView {
  ID: number
  ActivityName: string
  Color: string
  Period: string
  TargetMinutes: number
  TargetLabel: string
  AchievedMinutes: number
  AchievedLabel: string
  Percent: number
  PeriodStartLabel: string
  PeriodEndLabel: string
  PeriodRangeLabel: string
}

export interface GoalsData {
  Title: string
  Active: string
  ReactApp: boolean
  GoalsReact: boolean
  Lang: string
  CSRFToken: string
  CanManage: boolean
  Activities: Activity[]
  Goals: GoalView[]
}

export interface TagView {
  ID: number
  Name: string
  SessionCount: number
  Lang: string
}

export interface TagsData {
  Title: string
  Active: string
  ReactApp: boolean
  ReactTags: boolean
  Lang: string
  CSRFToken: string
  CanManage: boolean
  Tags: TagView[]
  AllTagNames: string[]
}

export interface GraphData {
  Title: string
  Active: string
  ReactApp: boolean
  GraphReact: boolean
  Lang: string
  Period: { Label: string; Start: string; End: string }
  PeriodStartInput: string
  PeriodEndInput: string
  Chart: { HasData: boolean; Hours: string[]; Series: Array<{ Name: string; Color: string; Data: number[]; Total: number }>; TotalLabel: string; Legend: Array<{ Name: string; Color: string }> }
  ChartJSON: string
  ProjectFilter: string
  ProjectName: string
  TagFilter: string
  PersonFilter: number
  PersonName: string
}

export interface TimesheetCell {
  Index: number
  ISO: string
  Secs: number
  Min: number
  Total: string
  IsToday: boolean
}

export interface TimesheetRow {
  ActivityID: number
  ActivityName: string
  Color: string
  Cells: TimesheetCell[]
  RowTotal: number
  RowTotalLabel: string
}

export interface TimesheetDay {
  Index: number
  Label: string
  Date: string
  ISO: string
  Secs: number
  Min: number
  Total: string
  IsToday: boolean
}

export interface TimesheetData {
  Title: string
  Active: string
  ReactApp: boolean
  TimesheetReact: boolean
  Lang: string
  CSRFToken: string
  Days: TimesheetDay[]
  Rows: TimesheetRow[]
  DayTotalLabels: string[]
  GrandTotal: number
  GrandTotalLabel: string
  Others: Activity[]
  Added: number[]
  DateISO: string
  PrevWeek: string
  NextWeek: string
  WeekLabel: string
}

export interface PayrollSummary {
  ID: number
  Number: string
  Status: string
  Total: string
  Hours: string
  Period: string
}

export interface PayrollData {
  Title: string
  Active: string
  ReactApp: boolean
  PayrollReact: boolean
  Lang: string
  CSRFToken: string
  Items: PayrollSummary[]
  DefStart: string
  DefEnd: string
  DefNotes: string
  Overlap: string
  Flash: string
  FlashOK: boolean
}

export interface PayrollDetailData {
  Title: string
  Active: string
  ReactApp: boolean
  PayrollReact: boolean
  PayrollDetail: boolean
  Lang: string
  CSRFToken: string
  Run: {
    ID: number
    Number: string
    Status: string
    Notes: string
    PeriodLabel: string
    Lines: Array<{ Label: string; Hours: string; Rate: string; Amount: string }>
    Total: string
    TotalCents: number
    Hours: string
  }
  Flash: string
  FlashOK: boolean
}

export interface ScheduleDay {
  Index: number
  Label: string
  Date: string
  ISO: string
  Min: number
  Total: string
  IsToday: boolean
}

export interface ScheduleRow {
  UserID: number
  UserName: string
  Capacity: number
  Cells: ScheduleDay[]
  Total: string
  TotalMin: number
  LoadPct: number
}

export interface ScheduleData {
  ReactApp: boolean
  ScheduleReact: boolean
  Lang: string
  CSRFToken: string
  CanManage: boolean
  WeekLabel: string
  PrevWeek: string
  NextWeek: string
  ThisWeek: string
  ProjectID: number
  Days: ScheduleDay[]
  Rows: ScheduleRow[]
  Projects: Project[]
  GrandTotal: string
  GrandMin: number
}

export type ReactPageData = DashboardData | ProjectListData | ProjectDetailData | ProjectCreateData | GoalsData | TagsData | GraphData | TimesheetData | PayrollData | PayrollDetailData | ScheduleData

export interface ReactPageBootstrap {
	data: ReactPageData
}
