"""Simple web UI for Better Track."""

from datetime import datetime, timedelta
from pathlib import Path

from flask import Flask, jsonify, redirect, render_template, request, url_for

from track.db import get_db

app = Flask(__name__, template_folder=str(Path(__file__).parent / "templates"))
app.secret_key = "dev-secret-key-change-in-production"


def format_duration_seconds(seconds: int) -> str:
    """Format duration in seconds to HH:MM:SS."""
    hours = seconds // 3600
    minutes = (seconds % 3600) // 60
    secs = seconds % 60
    return f"{hours:02d}:{minutes:02d}:{secs:02d}"


def calculate_session_duration(session) -> int:
    """Calculate session duration in seconds."""
    if session.end_at:
        # Closed session
        total_seconds = int((session.end_at - session.start_at).total_seconds())
        return total_seconds

    # Active session
    if session.paused:
        # Return accumulated time only
        return session.accumulated_seconds
    else:
        # Add current running time to accumulated
        now = datetime.now()
        resume_point = session.last_resume_at or session.start_at
        running_seconds = int((now - resume_point).total_seconds())
        return session.accumulated_seconds + running_seconds


def get_period_range(period: str, now: datetime = None):
    """Calculate start and end datetime for a given period.

    Args:
        period: Period name ('today', 'yesterday', 'week', 'month')
        now: Current datetime (defaults to datetime.now())

    Returns:
        Tuple of (start, end, period_label)
    """
    if now is None:
        now = datetime.now()

    if period == "today":
        start = now.replace(hour=0, minute=0, second=0, microsecond=0)
        end = now
        period_label = "Today"
    elif period == "yesterday":
        end = now.replace(hour=0, minute=0, second=0, microsecond=0)
        start = end - timedelta(days=1)
        period_label = "Yesterday"
    elif period == "week":
        start = (now - timedelta(days=now.weekday())).replace(
            hour=0, minute=0, second=0, microsecond=0
        )
        end = now
        period_label = "This Week"
    elif period == "month":
        start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)
        end = now
        period_label = "This Month"
    else:
        start = now.replace(hour=0, minute=0, second=0, microsecond=0)
        end = now
        period_label = "Today"

    return start, end, period_label


def clip_session_seconds(session, start: datetime, end: datetime) -> int:
    """Clip session duration to a time window.

    Args:
        session: Session object
        start: Window start time
        end: Window end time

    Returns:
        Clipped duration in seconds
    """
    s_start = max(session.start_at, start)
    s_end = min(session.end_at or end, end)
    return max(0, int((s_end - s_start).total_seconds()))


@app.route("/")
def index():
    """Home page showing active sessions."""
    db = get_db()
    active_sessions = db.get_active_sessions()

    # Format active sessions for display
    sessions_data = []
    for session, activity in active_sessions:
        duration = calculate_session_duration(session)
        status = "Paused" if session.paused else "Active"
        sessions_data.append({
            "id": session.id,
            "activity": activity.name,
            "start_at": session.start_at.strftime("%Y-%m-%d %H:%M"),
            "duration": format_duration_seconds(duration),
            "status": status,
            "note": session.note or "",
        })

    # Get all activities for the start form
    activities = db.list_activities(archived=False)

    return render_template(
        "index.html",
        sessions=sessions_data,
        activities=activities,
    )


@app.route("/start", methods=["POST"])
def start_activity():
    """Start tracking an activity."""
    db = get_db()
    activity_name = request.form.get("activity", "").strip()
    note = request.form.get("note", "").strip()

    if not activity_name:
        return redirect(url_for("index"))

    # Get or create activity
    activity = db.get_or_create_activity(activity_name)

    # Check for existing active session
    active = db.get_active_sessions()
    for session, act in active:
        if act.id == activity.id and session.end_at is None:
            # Already has active session
            return redirect(url_for("index"))

    # Create new session
    db.create_session(
        activity_id=activity.id,
        start_at=datetime.now(),
        note=note if note else None,
    )

    return redirect(url_for("index"))


@app.route("/stop/<int:session_id>", methods=["POST"])
def stop_session(session_id):
    """Stop a session."""
    db = get_db()
    db.update_session(session_id, end_at=datetime.now())
    return redirect(url_for("index"))


@app.route("/pause/<int:session_id>", methods=["POST"])
def pause_session(session_id):
    """Pause a session."""
    db = get_db()
    db.pause_session(session_id)
    return redirect(url_for("index"))


@app.route("/resume/<int:session_id>", methods=["POST"])
def resume_session(session_id):
    """Resume a session."""
    db = get_db()
    db.resume_session(session_id)
    return redirect(url_for("index"))


@app.route("/log")
def log():
    """View session log."""
    db = get_db()

    # Default to today
    period = request.args.get("period", "today")
    start, end, period_label = get_period_range(period)

    sessions = db.get_closed_sessions_overlapping(start, end, None)

    sessions_data = []
    total_seconds = 0
    for session, activity in sessions:
        sec = clip_session_seconds(session, start, end)
        if sec <= 0:
            continue
        total_seconds += sec
        sessions_data.append({
            "activity": activity.name,
            "start_at": session.start_at.strftime("%Y-%m-%d %H:%M"),
            "end_at": (session.end_at or end).strftime("%Y-%m-%d %H:%M"),
            "duration": format_duration_seconds(sec),
            "note": session.note or "",
        })

    return render_template(
        "log.html",
        sessions=sessions_data,
        total=format_duration_seconds(total_seconds),
        period=period,
        period_label=period_label,
        start=start.strftime("%Y-%m-%d %H:%M"),
        end=end.strftime("%Y-%m-%d %H:%M"),
    )


@app.route("/stats")
def stats():
    """View statistics."""
    db = get_db()

    # Default to today
    period = request.args.get("period", "today")
    start, end, period_label = get_period_range(period)

    sessions = db.get_closed_sessions_overlapping(start, end, None)

    # Clip and aggregate
    agg = {}
    total_seconds = 0
    for session, activity in sessions:
        sec = clip_session_seconds(session, start, end)
        if sec <= 0:
            continue
        total_seconds += sec
        agg[activity.name] = agg.get(activity.name, 0) + sec

    # Sort by duration descending
    stats_data = []
    for name, sec in sorted(agg.items(), key=lambda kv: kv[1], reverse=True):
        share = (sec / total_seconds * 100) if total_seconds else 0
        stats_data.append({
            "activity": name,
            "duration": format_duration_seconds(sec),
            "share": f"{share:.1f}%",
        })

    return render_template(
        "stats.html",
        stats=stats_data,
        total=format_duration_seconds(total_seconds),
        period=period,
        period_label=period_label,
        start=start.strftime("%Y-%m-%d %H:%M"),
        end=end.strftime("%Y-%m-%d %H:%M"),
    )


@app.route("/api/status")
def api_status():
    """API endpoint for active sessions status."""
    db = get_db()
    active_sessions = db.get_active_sessions()

    sessions_data = []
    for session, activity in active_sessions:
        duration = calculate_session_duration(session)
        sessions_data.append({
            "id": session.id,
            "activity": activity.name,
            "duration_seconds": duration,
            "duration": format_duration_seconds(duration),
            "status": "Paused" if session.paused else "Active",
        })

    return jsonify(sessions_data)


def run_server(host="127.0.0.1", port=8000, debug=False):
    """Run the Flask development server.

    Args:
        host: Host address to bind to (default: 127.0.0.1)
        port: Port to bind to (default: 8000)
        debug: Enable debug mode (default: False, use only in development)
    """
    app.run(host=host, port=port, debug=debug)


if __name__ == "__main__":
    import os
    # Only enable debug mode if explicitly set in environment
    debug_mode = os.getenv("FLASK_DEBUG", "false").lower() == "true"
    run_server(debug=debug_mode)
