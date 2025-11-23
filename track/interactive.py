"""Interactive menu utilities."""

from datetime import datetime

import questionary
from rich.console import Console
from rich.table import Table

from track.db import get_db
from track.models import Activity, Session

console = Console()


def select_activity(
    message: str = "Select activity",
    allow_new: bool = True,
    show_archived: bool = False,
) -> Activity | None:
    """Interactive activity selection."""
    db = get_db()
    activities = db.list_activities(archived=show_archived)

    if not activities and not allow_new:
        console.print("[yellow]No activities found[/yellow]")
        return None

    choices = [{"name": a.name, "value": a} for a in activities]

    if allow_new:
        choices.insert(0, {"name": "➕ Create new activity", "value": None})

    if not choices:
        return None

    result = questionary.select(message, choices=choices).ask()

    if result is None and allow_new:
        # User selected "Create new"
        name = questionary.text("Activity name:").ask()
        if name:
            return db.get_or_create_activity(name.strip())
        return None

    return result


def select_multiple_activities(
    message: str = "Select activities", show_archived: bool = False
) -> list[Activity]:
    """Interactive multiple activity selection."""
    db = get_db()
    activities = db.list_activities(archived=show_archived)

    if not activities:
        console.print("[yellow]No activities found[/yellow]")
        return []

    choices = [{"name": a.name, "value": a} for a in activities]

    results = questionary.checkbox(message, choices=choices).ask()
    return results if results else []


def select_session(
    sessions: list[Session], activities: dict[int, Activity], message: str = "Select session"
) -> Session | None:
    """Interactive session selection."""
    if not sessions:
        console.print("[yellow]No sessions found[/yellow]")
        return None

    choices = []
    for session in sessions:
        activity = activities.get(session.activity_id)
        activity_name = activity.name if activity else "Unknown"
        duration = format_duration(session)
        start_time = session.start_at.strftime("%Y-%m-%d %H:%M")

        label = f"{activity_name} - {start_time} ({duration})"
        if session.note:
            label += f" - {session.note[:30]}"

        choices.append({"name": label, "value": session})

    return questionary.select(message, choices=choices).ask()


def select_tags(message: str = "Select tags (space to toggle, enter to confirm)") -> list[str]:
    """Interactive tag selection."""
    db = get_db()
    existing_tags = db.list_tags()

    choices = [{"name": tag.name, "value": tag.name} for tag in existing_tags]
    choices.insert(0, {"name": "➕ Add new tags", "value": "__new__"})

    selected = questionary.checkbox(message, choices=choices).ask()

    if not selected:
        return []

    # Check if user wants to add new tags
    if "__new__" in selected:
        selected.remove("__new__")
        new_tags = questionary.text("Enter new tags (comma-separated):").ask()
        if new_tags:
            selected.extend([t.strip() for t in new_tags.split(",") if t.strip()])

    return selected


def confirm(message: str, default: bool = False) -> bool:
    """Interactive confirmation."""
    return questionary.confirm(message, default=default).ask()


def input_text(message: str, default: str = "") -> str:
    """Interactive text input."""
    return questionary.text(message, default=default).ask()


def input_time(message: str = "Enter time (e.g., '2 hours ago', 'yesterday 14:00', 'now')") -> str:
    """Interactive time input with natural language support."""
    return questionary.text(message, default="now").ask()


def input_duration(message: str = "Enter duration (e.g., '1.5h', '90m', '1h 30m')") -> str:
    """Interactive duration input."""
    return questionary.text(message).ask()


def select_period() -> str:
    """Interactive period selection."""
    choices = [
        {"name": "Today", "value": "today"},
        {"name": "Yesterday", "value": "yesterday"},
        {"name": "This week", "value": "week"},
        {"name": "Last week", "value": "last_week"},
        {"name": "This month", "value": "month"},
        {"name": "Last month", "value": "last_month"},
        {"name": "Custom range", "value": "custom"},
    ]
    return questionary.select("Select period:", choices=choices).ask()


def compute_session_seconds(session: Session) -> int:
    """Compute elapsed seconds for a session considering pause/resume semantics."""
    total = int(session.accumulated_seconds or 0)
    if session.end_at:
        # If already ended, add final leg from last_resume_at/start_at to end_at
        last_start = session.last_resume_at or session.start_at
        total += max(0, int((session.end_at - last_start).total_seconds()))
        return total
    # Still open
    if not session.paused:
        last_start = session.last_resume_at or session.start_at
        total += max(0, int((datetime.now() - last_start).total_seconds()))
    return total


def format_hhmmss(total_seconds: int) -> str:
    """Format total seconds as HH:MM:SS."""
    hours = total_seconds // 3600
    minutes = (total_seconds % 3600) // 60
    seconds = total_seconds % 60
    return f"{hours:02d}:{minutes:02d}:{seconds:02d}"


def display_active_sessions():
    """Display active sessions in a table."""
    db = get_db()
    active = db.get_active_sessions()

    if not active:
        console.print("[yellow]No active sessions[/yellow]")
        return

    table = Table(title="Active Sessions", show_header=True, header_style="bold magenta")
    table.add_column("Activity", style="cyan")
    table.add_column("Status", style="magenta")
    table.add_column("Started", style="green")
    table.add_column("Duration", style="yellow")
    table.add_column("Note", style="dim")

    total_seconds = 0
    for session, activity in active:
        started = session.start_at.strftime("%H:%M")
        # compute duration and accumulate
        sec = compute_session_seconds(session)
        total_seconds += sec
        duration = format_hhmmss(sec)
        note = session.note or ""

        status = "Paused" if session.paused else "Active"
        table.add_row(activity.name, status, started, duration, note)

    # Add total summary row only if there are multiple active sessions
    if len(active) > 1:
        table.add_row("", "", "", f"[bold]{format_hhmmss(total_seconds)}[/bold]", "")

    console.print(table)


def select_sessions_to_stop(active: list[tuple[Session, Activity]]) -> list[int]:
    """Interactive multiselect to choose which active sessions to stop.

    Returns list of selected session IDs.
    """
    if not active:
        console.print("[yellow]No active sessions[/yellow]")
        return []

    choices = []
    for session, activity in active:
        sec = compute_session_seconds(session)
        label = (
            f"{activity.name} | since {session.start_at.strftime('%H:%M')} | {format_hhmmss(sec)}"
        )
        if session.note:
            label += f" | {session.note[:20]}"
        choices.append({"name": label, "value": session.id})

    selected = questionary.checkbox(
        "Select sessions to stop (space to toggle, enter to confirm)", choices=choices
    ).ask()
    return selected or []


def display_sessions(sessions: list[tuple[Session, Activity]]):
    """Display sessions in a table."""
    if not sessions:
        console.print("[yellow]No sessions found[/yellow]")
        return

    table = Table(title="Sessions", show_header=True, header_style="bold magenta")
    table.add_column("ID", style="dim")
    table.add_column("Activity", style="cyan")
    table.add_column("Start", style="green")
    table.add_column("End", style="red")
    table.add_column("Duration", style="yellow")
    table.add_column("Note", style="dim")

    for session, activity in sessions:
        start = session.start_at.strftime("%Y-%m-%d %H:%M")
        end = session.end_at.strftime("%H:%M") if session.end_at else "active"
        duration = format_duration(session)
        note = (
            (session.note[:30] + "...")
            if session.note and len(session.note) > 30
            else (session.note or "")
        )

        table.add_row(str(session.id), activity.name, start, end, duration, note)

    console.print(table)
