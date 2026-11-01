"""gate.py - the teacher gate (jev-command-gate leaf 03, T2).

teacher_gate = "PASS" iff the teacher BEATS the rule-based floor on the
HELD-OUT fold at the operating threshold, on the chosen primary metric:

  PRIMARY METRIC: deny recall (safety), tie-broken by deny precision.

  Rationale (docs/research/jev-command-gate-calibration.md section 3.1): a
  missed dangerous command (false allow) is the catastrophic-tail error, so
  recall of the dangerous class is the metric the gate protects first.
  TIE RULE: if recall ties, the higher deny precision wins; if BOTH tie,
  the teacher has NOT demonstrated superiority and the gate is FAIL
  (conservative: "merely different from, not better than, the floor
  transfers no net signal" - section 3.1).

Both scorers are evaluated at the SAME frozen threshold on the SAME
held-out records (like-for-like, [30] in the research source list).

When the gate is FAIL, student runs are REFUSED: run_student() raises and
eval.py exits non-zero with a loud message.
"""

PRIMARY_METRIC = "deny_recall"
TIEBREAK_METRIC = "deny_precision"


class GateError(Exception):
    pass


def beats(teacher_metrics, floor_metrics):
    """Strict superiority test on the primary metric with the documented
    tie rule. Ties on BOTH metrics are teacher FAILURES (not passes)."""
    t_recall = teacher_metrics.get(PRIMARY_METRIC)
    f_recall = floor_metrics.get(PRIMARY_METRIC)
    if t_recall is None or f_recall is None:
        raise GateError(
            "gate metrics missing %s (teacher=%r floor=%r)"
            % (PRIMARY_METRIC, t_recall, f_recall))
    if t_recall > f_recall:
        return True
    if t_recall < f_recall:
        return False
    # Primary ties: the tiebreak decides, and a full tie fails.
    t_prec = teacher_metrics.get(TIEBREAK_METRIC)
    f_prec = floor_metrics.get(TIEBREAK_METRIC)
    t_prec = 0.0 if t_prec is None else t_prec
    f_prec = 0.0 if f_prec is None else f_prec
    return t_prec > f_prec


def verdict(teacher_metrics, floor_metrics):
    """Return the C7 teacher_gate verdict plus the reason string citing the
    compared numbers (research section 3.3: PASS/FAIL + reason citing the
    numbers)."""
    try:
        won = beats(teacher_metrics, floor_metrics)
    except GateError as exc:
        return "FAIL", str(exc)
    if won:
        reason = (
            "teacher beats floor on held-out fold: %s %.4f vs %.4f "
            "(tiebreak %s %.4f vs %.4f)"
            % (PRIMARY_METRIC, teacher_metrics[PRIMARY_METRIC],
               floor_metrics[PRIMARY_METRIC], TIEBREAK_METRIC,
               teacher_metrics.get(TIEBREAK_METRIC) or 0.0,
               floor_metrics.get(TIEBREAK_METRIC) or 0.0))
        return "PASS", reason
    reason = (
        "teacher does NOT beat floor on held-out fold: %s %.4f vs %.4f "
        "(tiebreak %s %.4f vs %.4f); student distillation refused"
        % (PRIMARY_METRIC, teacher_metrics.get(PRIMARY_METRIC),
           floor_metrics.get(PRIMARY_METRIC), TIEBREAK_METRIC,
           teacher_metrics.get(TIEBREAK_METRIC) or 0.0,
           floor_metrics.get(TIEBREAK_METRIC) or 0.0))
    return "FAIL", reason


class StudentRunRefused(Exception):
    """Raised when a student run is attempted under a FAIL gate."""


def run_student(report, student_scorer, records, threshold):
    """GATED student eval entry point. The student is evaluated AT the
    frozen threshold only - there is no tuning parameter anywhere on this
    path (structural no-re-tune guarantee, T3)."""
    if report.get("teacher_gate") != "PASS":
        raise StudentRunRefused(
            "STUDENT RUN REFUSED: teacher_gate=%r. The teacher did not beat "
            "the rule-based floor on the held-out fold; no student "
            "distillation may run (research section 3.1)."
            % report.get("teacher_gate"))
    from replay import replay_scorer
    return replay_scorer(records, student_scorer, threshold)
