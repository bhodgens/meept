"""Multi-instance harness for self-improvement testing.

Allows running a "tester" daemon that debugs/tests a "subject" daemon.
"""

from meept.selfimprove.harness.tester_daemon import TesterDaemon
from meept.selfimprove.harness.subject_daemon import SubjectDaemon
from meept.selfimprove.harness.rpc_bridge import RpcBridge

__all__ = [
    "TesterDaemon",
    "SubjectDaemon",
    "RpcBridge",
]
