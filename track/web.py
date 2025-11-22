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
        sessions_data.append(
            {
                "id": session.id,
                "activity": activity.name,
                "start_at": session.start_at.strftime("%Y-%m-%d %H:%M"),
                "duration": format_duration_seconds(duration),
                "status": status,
                "note": session.note or "",
            }
        )

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


@app.route("/stats")
def stats():
    """View statistics with detailed sessions."""
    db = get_db()

    # Default to today
    period = request.args.get("period", "today")
    start, end, period_label = get_period_range(period)

    sessions = db.get_closed_sessions_overlapping(start, end, None)

    # Prepare session data with all details
    sessions_data = []
    total_seconds = 0
    for session, activity in sessions:
        sec = clip_session_seconds(session, start, end)
        if sec <= 0:
            continue
        total_seconds += sec
        sessions_data.append(
            {
                "id": session.id,
                "activity": activity.name,
                "start_at": session.start_at.strftime("%Y-%m-%d %H:%M:%S"),
                "start_at_iso": session.start_at.strftime("%Y-%m-%dT%H:%M:%S"),
                "end_at": (session.end_at or end).strftime("%Y-%m-%d %H:%M:%S"),
                "end_at_iso": (session.end_at or end).strftime("%Y-%m-%dT%H:%M:%S"),
                "duration": format_duration_seconds(sec),
                "duration_seconds": sec,
                "note": session.note or "",
            }
        )

    return render_template(
        "stats.html",
        sessions=sessions_data,
        total=format_duration_seconds(total_seconds),
        period=period,
        period_label=period_label,
        start=start.strftime("%Y-%m-%d %H:%M"),
        end=end.strftime("%Y-%m-%d %H:%M"),
    )


@app.route("/update_session/<int:session_id>", methods=["POST"])
def update_session(session_id):
    """Update session start, end, or duration."""
    db = get_db()

    # Get the specific session efficiently
    sessions = db.get_closed_sessions_overlapping(
        datetime(2000, 1, 1), datetime.now() + timedelta(days=1), None
    )
    session = None
    for s, _a in sessions:
        if s.id == session_id:
            session = s
            break

    if not session:
        return jsonify({"error": "Session not found"}), 404

    # Get form data
    start_str = request.form.get("start_at")
    end_str = request.form.get("end_at")
    duration_str = request.form.get("duration")
    note = request.form.get("note")

    try:
        # Update note if provided
        update_params = {}
        if note is not None:
            update_params["note"] = note if note.strip() else None

        if start_str and end_str:
            # Parse datetime strings (remove timezone info for naive datetime)
            start_dt = datetime.fromisoformat(
                start_str.replace("Z", "").replace("+00:00", "")
            )
            end_dt = datetime.fromisoformat(
                end_str.replace("Z", "").replace("+00:00", "")
            )

            if end_dt <= start_dt:
                return jsonify({"error": "End time must be after start time"}), 400

            # Update session
            update_params["start_at"] = start_dt
            update_params["end_at"] = end_dt
            db.update_session(session_id, **update_params)
        elif duration_str:
            # Parse and validate duration (HH:MM:SS format)
            parts = duration_str.split(":")
            if len(parts) != 3:
                return jsonify({"error": "Duration must be in HH:MM:SS format"}), 400

            try:
                hours, minutes, seconds = map(int, parts)
                if (
                    hours < 0
                    or minutes < 0
                    or minutes >= 60
                    or seconds < 0
                    or seconds >= 60
                ):
                    return jsonify({"error": "Invalid time values"}), 400

                new_duration = timedelta(hours=hours, minutes=minutes, seconds=seconds)
                if new_duration.total_seconds() == 0:
                    return jsonify({"error": "Duration must be greater than zero"}), 400

                # Use new start time if provided, otherwise use existing
                if start_str:
                    base_start = datetime.fromisoformat(
                        start_str.replace("Z", "").replace("+00:00", "")
                    )
                    update_params["start_at"] = base_start
                else:
                    base_start = session.start_at

                new_end = base_start + new_duration
                update_params["end_at"] = new_end
                db.update_session(session_id, **update_params)
            except ValueError:
                return jsonify({"error": "Duration must contain only numbers"}), 400
        elif update_params:
            # Only updating note
            db.update_session(session_id, **update_params)

        return jsonify({"success": True})
    except Exception as e:
        return jsonify({"error": str(e)}), 400


@app.route("/delete_session/<int:session_id>", methods=["POST"])
def delete_session(session_id):
    """Delete a session."""
    db = get_db()
    try:
        db.delete_session(session_id)
        return jsonify({"success": True})
    except Exception as e:
        return jsonify({"error": str(e)}), 400


@app.route("/graph")
def graph():
    """View daily distribution graph."""
    db = get_db()

    # Default to this week
    period = request.args.get("period", "week")
    start, end, period_label = get_period_range(period)

    sessions = db.get_closed_sessions_overlapping(start, end, None)

    # Group sessions by day and check for overlaps
    from collections import defaultdict

    daily_data = defaultdict(lambda: {"activities": [], "overlaps": []})

    for session, activity in sessions:
        if not session.end_at:
            continue

        session_start = max(session.start_at, start)
        session_end = min(session.end_at, end)

        # Get the day
        day_key = session_start.strftime("%Y-%m-%d")

        daily_data[day_key]["activities"].append(
            {
                "name": activity.name,
                "start": session_start,
                "end": session_end,
                "duration": int((session_end - session_start).total_seconds()),
            }
        )

    # Check for overlaps within each day
    for _day_key, data in daily_data.items():
        activities = sorted(data["activities"], key=lambda x: x["start"])
        # Mark activities that have overlaps
        overlapping_indices = set()
        for i in range(len(activities)):
            for j in range(i + 1, len(activities)):
                a1, a2 = activities[i], activities[j]
                # Check if they truly overlap: a1 must end after a2 starts AND a1 must start before a2 ends
                if a1["end"] > a2["start"] and a1["start"] < a2["end"]:
                    overlapping_indices.add(i)
                    overlapping_indices.add(j)
                    overlap_start = max(a1["start"], a2["start"])
                    overlap_end = min(a1["end"], a2["end"])
                    overlap_seconds = int((overlap_end - overlap_start).total_seconds())
                    if overlap_seconds > 0:
                        data["overlaps"].append(
                            {
                                "activities": [a1["name"], a2["name"]],
                                "start": overlap_start,
                                "end": overlap_end,
                                "duration": overlap_seconds,
                            }
                        )
        # Mark which activities have overlaps
        for i, activity in enumerate(activities):
            activity["has_overlap"] = i in overlapping_indices

    # Prepare data for template
    graph_data = []
    for day_key in sorted(daily_data.keys()):
        data = daily_data[day_key]
        total_seconds = sum(a["duration"] for a in data["activities"])
        graph_data.append(
            {
                "date": day_key,
                "activities": data["activities"],
                "overlaps": data["overlaps"],
                "total": format_duration_seconds(total_seconds),
                "has_overlaps": len(data["overlaps"]) > 0,
            }
        )

    return render_template(
        "graph.html",
        graph_data=graph_data,
        period=period,
        period_label=period_label,
        start=start.strftime("%Y-%m-%d"),
        end=end.strftime("%Y-%m-%d"),
    )


@app.route("/api/status")
def api_status():
    """API endpoint for active sessions status."""
    db = get_db()
    active_sessions = db.get_active_sessions()

    sessions_data = []
    for session, activity in active_sessions:
        duration = calculate_session_duration(session)
        sessions_data.append(
            {
                "id": session.id,
                "activity": activity.name,
                "duration_seconds": duration,
                "duration": format_duration_seconds(duration),
                "status": "Paused" if session.paused else "Active",
            }
        )

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
