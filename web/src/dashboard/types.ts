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
  PersonName?: string
  Color: string
  ProjectID: number
  ProjectName: string
  ProjectColor: string
  ProjectSlug: string
  StartISO: string
  StartInput?: string
  EndInput?: string
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

export interface InvoiceSummary {
  ID: number
  Number: string
  Client: string
  Status: string
  Total: string
  Hours: string
  Period: string
}

export interface InvoiceProjectOption {
  ID: number
  Name: string
  ClientName: string
  ClientDetails: string
  ClientEmail: string
  Selected: boolean
  Eligible: boolean
}

export interface InvoicesData {
  ReactApp: boolean
  InvoicesReact: boolean
  Lang: string
  CSRFToken: string
  Billable: boolean
  Items: InvoiceSummary[]
  Projects: InvoiceProjectOption[]
  Prefill: InvoiceProjectOption
  Unbilled: Array<{ ProjectID: number; ProjectName: string; Slug: string; Hours: string; Amount: string; Since: string; SinceISO: string }>
  Unassigned: Array<{ ID: number; Sessions: number; Name: string; Billed: boolean }>
  DefStart: string
  DefEnd: string
  Flash: string
  FlashOK: boolean
}

export interface InvoiceDetailData {
  ReactApp: boolean
  InvoiceReact: boolean
  InvoiceDetail: boolean
  Lang: string
  CSRFToken: string
  Inv: {
    ID: number
    Number: string
    ClientName: string
    PeriodLabel: string
    PeriodISO: string
    Status: string
    Notes: string
    Lines: Array<{ Label: string; Hours: string; Rate: string; Amount: string }>
    Total: string
    Hours: string
    PaymentURL: string
    Currency: string
    IssuedLabel: string
    SellerDetails: string
    ClientDetails: string
    VATNote: string
    ClientEmail: string
    Receipt: string
    Logo: string
  }
  Seller: string
  StripeReady: boolean
  MailReady: boolean
  MailtoURL: string
  Flash: string
  FlashOK: boolean
}

export interface StatsData {
  Title: string
  Active: string
  Lang: string
  CSRFToken: string
  CanManage: boolean
  Mods: Record<string, boolean>
  Period: { Start: string; End: string; Label: string }
  Aggregated: Array<{ ActivityName: string; Color: string; Duration: string; Share: number }>
  ByProject: Array<{ ProjectID: number; ProjectName: string; Slug: string; Color: string; Duration: string; Share: number; Activities: Array<{ ActivityName: string; Color: string; Duration: string; Share: number }> }>
  Projects: Project[]
  ProjectFilter: string
  People: Array<{ ID: number; Name: string; Selected: boolean }>
  PersonFilter: number
  Sessions: Session[]
  Total: string
  SessionCount: number
  SessionsCut: boolean
  MeID: number
  ShowAllURL: string
  TagFilter: string
  AllTagNames: string[]
  SavedReports: Array<{ ID: number; Name: string; Period: string; ProjectSlug: string; Tag: string; CreatedBy: number }>
}

export interface ReportsData {
  Active: string
  Lang: string
  ReportsReact: boolean
  Templates: Array<{ ID: string; Name: string; Blurb: string; Icon: string }>
  DefFrom: string
  DefTo: string
}

export interface ReportRunData {
  Active: string
  Lang: string
  CSRFToken: string
  ReportRunReact: boolean
  VM: {
    Template: { id: string; name: string; billable: boolean }
    PeriodLabel: string
    From: string
    To: string
    Rows: Array<{ Key: string; Hours: string; Rate: string; Amount: string; Share: number }>
    Total: string
    TotalAmount: string
  }
}

export interface ExportData {
  Active: string
  Lang: string
  CanManage: boolean
  ReportsEnabled: boolean
}

export interface IntegrationsData {
  Active: string
  Lang: string
  CSRFToken: string
  IntegrationsReact: boolean
  Items: Array<{ ID: number; Provider: string; Name: string; TaskCount: number }>
  Flash: string
  FlashOK: boolean
}

export interface IntegrationDetailData {
  Active: string
  Lang: string
  CSRFToken: string
  IntegrationReact: boolean
  Integration: { ID: number; Provider: string; Name: string; TaskCount: number }
  Tasks: Array<{ ID: number; Title: string; URL: string; Status: string; ExternalID: string }>
  Flash: string
  FlashOK: boolean
}

export interface MarketplaceData {
  Active: string
  Lang: string
  MarketReact: boolean
  Items: Array<{ ID: string; Name: string; Category: string; Icon: string; Blurb: string; Available: boolean; Connected: boolean; SecretHint: string; TargetHint: string }>
}

export interface TokensData {
  Active: string
  Lang: string
  CSRFToken: string
  CanManage: boolean
  TokensReact: boolean
  Tokens: Array<{ ID: number; Name: string; Prefix: string; Created: string; Expires: string; Expired: boolean; Team: string; ReadOnly: boolean }>
  JustCreated: string
  Flash: string
  FlashOK: boolean
}

export interface ProfileData {
  Active: string
  Lang: string
  CSRFToken: string
  CanManage: boolean
  ProfileReact: boolean
  User: { ID: number; Email: string; Name: string }
  Flash: string
  FlashOK: boolean
}

export interface PreferencesData {
  Active: string
  Lang: string
  CSRFToken: string
  CanManage: boolean
  PrefsReact: boolean
  Sections: Array<{ Key: string; Icon: string; Label: string; On: boolean }>
  TabOpts: Array<{ Key: string; Icon: string; Label: string; On: boolean }>
  Widgets: Array<{ Key: string; Icon: string; Label: string; On: boolean }>
  P: { Duration: string; WeekStart: string; TZ: string }
  Zones: string[]
  Projects: Array<{ ID: number; Name: string }>
  DefProj: number
  Flash: string
  FlashOK: boolean
}

export type ReactPageData = DashboardData | ProjectListData | ProjectDetailData | ProjectCreateData | GoalsData | TagsData | GraphData | TimesheetData | PayrollData | PayrollDetailData | ScheduleData | InvoicesData | InvoiceDetailData | StatsData | ReportsData | ReportRunData | ExportData | IntegrationsData | IntegrationDetailData | MarketplaceData | TokensData | ProfileData | PreferencesData

export interface ReactPageBootstrap {
	data: ReactPageData
}
