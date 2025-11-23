"""CLI commands and entry point."""

import sys
from datetime import datetime, timedelta

import dateparser
import questionary
import typer
from rich.console import Console
from rich.table import Table

from track.db import get_db
from track.interactive import (
    confirm,
    display_active_sessions,
    input_text,
    select_activity,
    select_period,
    select_sessions_to_stop,
)

app = typer.Typer(
    name="track",
    help="Minimalist CLI time tracker with parallel activity tracking",
    add_completion=False,
)
console = Console()


def _start_activity_flow(activity: str | None):
    """Shared flow for starting an activity."""
    db = get_db()

    if activity:
        # Direct start
        act = db.get_or_create_activity(activity)
    else:
        # Interactive selection
        act = select_activity("Select or create activity:", allow_new=True)
        if not act:
            console.print("[yellow]Cancelled[/yellow]")
            return

    # Do not allow multiple active sessions for the same activity
    existing_same = [
        session
        for session, _a in db.get_active_sessions()
        if session.activity_id == act.id and session.end_at is None
    ]
    if existing_same:
        console.print(
            f"[yellow]Activity '[cyan]{act.name}[/cyan]' already has an active session. Use 'track pause {act.name}' or 'track stop {act.name}'.[/yellow]"
        )
        return

    # Ask for note
    note = input_text("Add note (optional):", default="")

    # Create session
    session = db.create_session(
        activity_id=act.id,
        start_at=datetime.now(),
        note=note if note else None,
    )

    console.print(f"[green]✓[/green] Started tracking [cyan]{act.name}[/cyan]")
    if note:
        console.print(f"  Note: {note}")


@app.command()
def stop(activity: str | None = typer.Argument(None, help="Activity name to stop")):
    """Stop tracking activities (alias: s)."""
    db = get_db()
    active = db.get_active_sessions()

    if not active:
        console.print("[yellow]No active sessions[/yellow]")
        return

    if activity:
        # Stop specific activity
        found = False
        for session, act in active:
            if act.name.lower() == activity.lower():
                db.update_session(session.id, end_at=datetime.now())
                console.print(f"[green]✓[/green] Stopped [cyan]{act.name}[/cyan]")
                found = True
                break
        if not found:
            console.print(f"[red]Activity '{activity}' is not active[/red]")
    else:
        # Interactive selection
        if len(active) == 1:
            # Only one active, stop it
            session, act = active[0]
            if confirm(f"Stop tracking '{act.name}'?", default=True):
                db.update_session(session.id, end_at=datetime.now())
                console.print(f"[green]✓[/green] Stopped [cyan]{act.name}[/cyan]")
        else:
            # Multiple active, show selection UI
            console.print("[yellow]Multiple active sessions.[/yellow]")
            selection = select_sessions_to_stop(active)
            if not selection:
                console.print("[yellow]No sessions selected[/yellow]")
                return
            for sid in selection:
                db.update_session(sid, end_at=datetime.now())
            console.print(f"[green]✓[/green] Stopped {len(selection)} session(s)")


@app.command()
def pause(activity: str | None = typer.Argument(None, help="Activity name to pause")):
    """Pause tracking activities (alias: p)."""
    db = get_db()
    active = db.get_active_sessions()
    if not active:
        console.print("[yellow]No active sessions[/yellow]")
        return
    count = 0
    for session, act in active:
        if activity is None or act.name.lower() == activity.lower():
            if session.end_at is None and not session.paused:
                db.pause_session(session.id)
                count += 1
    if count:
        console.print(f"[green]✓[/green] Paused {count} session(s)")
    else:
        console.print("[yellow]Nothing to pause[/yellow]")


@app.command()
def resume(activity: str | None = typer.Argument(None, help="Activity name to resume")):
    """Resume paused activities (alias: r)."""
    db = get_db()
    active = db.get_active_sessions()
    if not active:
        console.print("[yellow]No sessions[/yellow]")
        return
    count = 0
    # If activity provided, resume only those paused sessions of that activity; otherwise resume all paused
    for session, act in active:
        if activity is None or act.name.lower() == activity.lower():
            if session.end_at is None and session.paused:
                db.resume_session(session.id)
                count += 1
    if count:
        console.print(f"[green]✓[/green] Resumed {count} session(s)")
    else:
        console.print("[yellow]Nothing to resume[/yellow]")


@app.command()
def focus(activity: str | None = typer.Argument(None, help="Activity to focus")):
    """Focus on one activity: pause others, resume or start the selected."""
    db = get_db()
    # Resolve target activity
    if activity is None:
        act_obj = select_activity("Select activity to focus:", allow_new=False)
        if not act_obj:
            console.print("[yellow]Cancelled[/yellow]")
            return
        target_name = act_obj.name
    else:
        target_name = activity

    active = db.get_active_sessions()
    target_has_any = False
    resumed = 0
    paused = 0
    for session, act in active:
        if act.name.lower() == target_name.lower():
            target_has_any = True
            if session.paused and session.end_at is None:
                db.resume_session(session.id)
                resumed += 1
        else:
            if session.end_at is None and not session.paused:
                db.pause_session(session.id)
                paused += 1

    # If no session exists for target activity, start one
    if not target_has_any:
        _start_activity_flow(target_name)
        console.print(
            f"[green]✓[/green] Focus started on [cyan]{target_name}[/cyan]; paused {paused} other(s)"
        )
        return

    console.print(
        f"[green]✓[/green] Focus on [cyan]{target_name}[/cyan]: resumed {resumed}, paused {paused} other(s)"
    )


@app.command()
def switch(activity: str | None = typer.Argument(None, help="Activity to switch/focus")):
    """Switch context: pause others, resume/start selected (alias: sw)."""
    focus(activity)


@app.command()
def status():
    """Show active sessions (alias: st)."""
    display_active_sessions()


@app.command()
def add():
    """Add a past session (alias: a)."""
    db = get_db()

    # 1) Choose or create activity
    act = select_activity("Select activity for the past session:", allow_new=True)
    if not act:
        console.print("[yellow]Cancelled[/yellow]")
        return

    # 2) Enter start time (natural language)
    start_str = input_text(
        "When did it start? (e.g., '2 hours ago', 'yesterday 14:00')",
        default="2 hours ago",
    )
    start_dt = dateparser.parse(start_str) if start_str else None
    if not start_dt:
        console.print(f"[red]Cannot parse start time: {start_str}[/red]")
        return

    # 3) Choose input mode: duration or end time
    mode = questionary.select(
        "Provide duration or end time?",
        choices=[
            {"name": "Duration (e.g., 1.5h, 90m, 1h 30m)", "value": "duration"},
            {"name": "End time (e.g., 'yesterday 16:30')", "value": "end"},
        ],
    ).ask()
    if not mode:
        console.print("[yellow]Cancelled[/yellow]")
        return

    def parse_duration_to_seconds(text: str) -> int | None:
        text = (text or "").strip().lower()
        if not text:
            return None
        total = 0
        num = ""
        unit = ""
        i = 0
        tokens: list[tuple[float, str]] = []
        # simple tokenizer for patterns like '1.5h', '1h 30m', '90m'
        while i < len(text):
            ch = text[i]
            if ch.isdigit() or ch == ".":
                num += ch
                i += 1
                continue
            if ch.isspace():
                i += 1
                continue
            # read unit letters
            unit = ch
            i += 1
            while i < len(text) and text[i].isalpha():
                unit += text[i]
                i += 1
            try:
                val = float(num) if num else 0.0
            except ValueError:
                return None
            tokens.append((val, unit))
            num = ""
            unit = ""
        # handle case where it's a single number without unit => minutes
        if tokens == [] and num:
            try:
                val = float(num)
            except ValueError:
                return None
            tokens.append((val, "m"))

        for val, u in tokens:
            if u in ("h", "hr", "hrs", "hour", "hours"):
                total += int(val * 3600)
            elif u in ("m", "min", "mins", "minute", "minutes"):
                total += int(val * 60)
            elif u in ("s", "sec", "secs", "second", "seconds"):
                total += int(val)
            else:
                # unknown unit
                return None
        return total if total > 0 else None

    end_dt = None
    if mode == "duration":
        dur_str = input_text("Duration:", default="1h")
        seconds = parse_duration_to_seconds(dur_str)
        if not seconds:
            console.print(f"[red]Cannot parse duration: {dur_str}[/red]")
            return
        end_dt = start_dt + timedelta(seconds=seconds)
    else:
        end_str = input_text("End time (e.g., 'yesterday 16:30'):")
        end_dt = dateparser.parse(end_str) if end_str else None
        if not end_dt:
            console.print(f"[red]Cannot parse end time: {end_str}[/red]")
            return
        if end_dt <= start_dt:
            console.print("[red]End time must be after start time[/red]")
            return

    # 4) Optional note
    note = input_text("Note (optional):", default="")

    # 5) Create session
    session = db.create_session(act.id, start_at=start_dt, end_at=end_dt, note=note or None)

    console.print(
        f"[green]✓[/green] Added session [cyan]{act.name}[/cyan] "
        f"{start_dt.strftime('%Y-%m-%d %H:%M')} → {end_dt.strftime('%Y-%m-%d %H:%M')}"
    )


@app.command()
def edit():
    """Edit a session (alias: e)."""
    console.print("[yellow]Session editing - coming soon![/yellow]")


@app.command()
def delete():
    """Delete a session (alias: d)."""
    console.print("[yellow]Session deletion - coming soon![/yellow]")


@app.command()
def log():
    """View session history (alias: l)."""
    db = get_db()
    # Select period
    period = select_period()
    now = datetime.now()
    if period == "today":
        start = now.replace(hour=0, minute=0, second=0, microsecond=0)
        end = now
    elif period == "yesterday":
        end = now.replace(hour=0, minute=0, second=0, microsecond=0)
        start = end - timedelta(days=1)
    elif period == "week":
        # start of this week (Mon)
        start = (now - timedelta(days=now.weekday())).replace(
            hour=0, minute=0, second=0, microsecond=0
        )
        end = now
    elif period == "last_week":
        end_of_last_week = (now - timedelta(days=now.weekday() + 1)).replace(
            hour=23, minute=59, second=59, microsecond=0
        )
        start = end_of_last_week - timedelta(days=6)
        end = now
    elif period == "month":
        start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        end = now
    elif period == "last_month":
        first_this = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        last_month_end = first_this - timedelta(seconds=1)
        start = last_month_end.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        end = last_month_end
    else:
        # custom
        start_str = input_text("Start (e.g., '2025-10-01 09:00'/'2 days ago'):")
        end_str = input_text("End (e.g., '2025-10-02 18:00'/'yesterday 23:59'):")
        start = dateparser.parse(start_str) if start_str else None
        end = dateparser.parse(end_str) if end_str else None
        if not start or not end or end <= start:
            console.print("[red]Invalid custom period[/red]")
            return

    # Optional activity filter
    act = select_activity("Filter by activity (optional):", allow_new=False)
    act_id = act.id if act else None

    sessions = db.get_closed_sessions_overlapping(start, end, act_id)
    if not sessions:
        console.print("[yellow]No sessions in selected period[/yellow]")
        return

    # clip to window and display
    def clip_seconds(s):
        s_start = max(s.start_at, start)
        s_end = min(s.end_at or end, end)
        return max(0, int((s_end - s_start).total_seconds()))

    table = Table(
        title=f"Log {start.strftime('%Y-%m-%d %H:%M')} → {end.strftime('%Y-%m-%d %H:%M')}"
    )
    table.add_column("Activity", style="cyan")
    table.add_column("Start", style="green")
    table.add_column("End", style="red")
    table.add_column("Duration", style="yellow")
    table.add_column("Note", style="dim")
    total = 0
    for session, activity in sessions:
        sec = clip_seconds(session)
        if sec <= 0:
            continue
        total += sec
        table.add_row(
            activity.name,
            session.start_at.strftime("%Y-%m-%d %H:%M"),
            (session.end_at or end).strftime("%Y-%m-%d %H:%M"),
            f"{sec // 3600:02d}:{(sec % 3600) // 60:02d}:{sec % 60:02d}",
            session.note or "",
        )
    table.add_row(
        "",
        "",
        "",
        f"[bold]{total // 3600:02d}:{(total % 3600) // 60:02d}:{total % 60:02d}[/bold]",
        "",
    )
    console.print(table)


@app.command()
def sessions():
    """View activity sessions (alias: ss)."""
    console.print("[yellow]Activity sessions - coming soon![/yellow]")


@app.command()
def stats():
    """View statistics (alias: st)."""
    db = get_db()
    period = select_period()
    now = datetime.now()
    if period == "today":
        start = now.replace(hour=0, minute=0, second=0, microsecond=0)
        end = now
    elif period == "yesterday":
        end = now.replace(hour=0, minute=0, second=0, microsecond=0)
        start = end - timedelta(days=1)
    elif period == "week":
        start = (now - timedelta(days=now.weekday())).replace(
            hour=0, minute=0, second=0, microsecond=0
        )
        end = now
    elif period == "last_week":
        end_of_last_week = (now - timedelta(days=now.weekday() + 1)).replace(
            hour=23, minute=59, second=59, microsecond=0
        )
        start = end_of_last_week - timedelta(days=6)
        end = now
    elif period == "month":
        start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        end = now
    elif period == "last_month":
        first_this = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        last_month_end = first_this - timedelta(seconds=1)
        start = last_month_end.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        end = last_month_end
    else:
        start_str = input_text("Start (custom):")
        end_str = input_text("End (custom):")
        start = dateparser.parse(start_str) if start_str else None
        end = dateparser.parse(end_str) if end_str else None
        if not start or not end or end <= start:
            console.print("[red]Invalid custom period[/red]")
            return

    sessions = db.get_closed_sessions_overlapping(start, end, None)

    # aggregate per activity with clipping
    def clip_seconds(s):
        s_start = max(s.start_at, start)
        s_end = min(s.end_at or end, end)
        return max(0, int((s_end - s_start).total_seconds()))

    agg: dict[str, int] = {}
    total = 0
    for session, activity in sessions:
        sec = clip_seconds(session)
        if sec <= 0:
            continue
        total += sec
        agg[activity.name] = agg.get(activity.name, 0) + sec

    table = Table(
        title=f"Stats {start.strftime('%Y-%m-%d %H:%M')} → {end.strftime('%Y-%m-%d %H:%M')}"
    )
    table.add_column("Activity", style="cyan")
    table.add_column("Time", style="yellow")
    table.add_column("Share", style="magenta")
    for name, sec in sorted(agg.items(), key=lambda kv: kv[1], reverse=True):
        share = (sec / total * 100) if total else 0
        table.add_row(
            name, f"{sec // 3600:02d}:{(sec % 3600) // 60:02d}:{sec % 60:02d}", f"{share:.1f}%"
        )
    table.add_row(
        "", f"[bold]{total // 3600:02d}:{(total % 3600) // 60:02d}:{total % 60:02d}[/bold]", ""
    )
    console.print(table)


@app.command()
def report():
    """Generate report (alias: rep)."""
    console.print("[yellow]Reports - coming soon![/yellow]")


@app.command()
def export():
    """Export data (alias: ex)."""
    console.print("[yellow]Data export - coming soon![/yellow]")


@app.command()
def goal():
    """Manage goals (alias: g)."""
    console.print("[yellow]Goals - coming soon![/yellow]")


@app.command()
def remind():
    """Manage reminders (alias: rm)."""
    console.print("[yellow]Reminders - coming soon![/yellow]")


@app.command()
def tag():
    """Manage tags (alias: tg)."""
    console.print("[yellow]Tag management - coming soon![/yellow]")


@app.command()
def config():
    """Manage configuration (alias: cfg)."""
    console.print("[yellow]Configuration - coming soon![/yellow]")


@app.callback(invoke_without_command=True)
def main(ctx: typer.Context):
    """Track - Minimalist CLI time tracker."""
    if ctx.invoked_subcommand is not None:
        return

    # No command provided, show status dashboard
    console.print("[bold cyan]Track CLI[/bold cyan]")
    console.print()
    display_active_sessions()
    console.print()
    console.print("Run [cyan]track --help[/cyan] to see available commands")


def run():
    """Entry point for console script."""
    argv = sys.argv[1:]
    if argv:
        first = argv[0]
        # Map short aliases to full command names for Typer
        alias_map = {
            "s": "stop",
            "p": "pause",
            "r": "resume",
            "st": "status",
            "a": "add",
            "e": "edit",
            "d": "delete",
            "l": "log",
            "ss": "sessions",
            "sw": "switch",
            "rep": "report",
            "ex": "export",
            "g": "goal",
            "rm": "remind",
            "tg": "tag",
            "cfg": "config",
        }

        # Known commands (explicit list) + aliases
        known_commands = {
            "track",
            "stop",
            "pause",
            "resume",
            "status",
            "add",
            "edit",
            "delete",
            "log",
            "sessions",
            "stats",
            "report",
            "export",
            "goal",
            "remind",
            "tag",
            "config",
            "focus",
            "switch",
        } | set(alias_map.keys())

        # If alias used, replace it so Typer can parse correctly
        if first in alias_map:
            sys.argv[1] = alias_map[first]
            first = sys.argv[1]

        # If it's a known command or an option, delegate to Typer
        if first in known_commands or first.startswith("-"):
            return app()

        # Otherwise treat as activity quick-start
        _start_activity_flow(first)
        return

    app()


if __name__ == "__main__":
    run()
