"""Offline wire-contract fixtures; no daemon, model, or optional deps needed."""
import asyncio
import builtins
import json
import threading
import unittest
from unittest.mock import Mock, patch

import async_transport as transport
import async_wait
import client


class Socket:
    def __init__(self):
        self.queue = None
        self.loop = None
        self.ready = False
        self.closed = False
        self.terminal_read = threading.Event()

    async def __aenter__(self):
        self.loop = asyncio.get_running_loop()
        self.queue = asyncio.Queue()
        return self

    async def __aexit__(self, *args):
        self.closed = True

    async def send(self, raw):
        frame = json.loads(raw)
        if frame['type'] == 'subscribe':
            await self.queue.put(json.dumps({'type': 'subscribed', 'data': {'channel': 'all'}}))
        elif frame['type'] == 'ping':
            self.ready = True
            await self.queue.put(json.dumps({'type': 'pong'}))

    async def recv(self):
        item = await self.queue.get()
        if isinstance(item, Exception):
            raise item
        if json.loads(item).get("data", {}).get("status") == "completed":
            self.terminal_read.set()
        return item

    def emit(self, event):
        item = event if isinstance(event, Exception) else json.dumps(
            {'type': 'agent_progress', 'data': event})
        self.loop.call_soon_threadsafe(self.queue.put_nowait, item)


def event(payload, **changes):
    value = {k: payload[k] for k in ('turn_id', 'session_id', 'conversation_id')}
    value.update(source_topic='turn.terminal', task_id='task-fixture',
                 status='completed', reply='391', error='')
    value.update(changes)
    return value


class TransportTest(unittest.TestCase):
    def make_client(self):
        c = client.Client.__new__(client.Client)
        c.base, c.key, c.ctx, c.timeout = 'http://fixture.invalid', 'fixture', None, 0.1
        c.transport = 'async'
        return c

    def exercise(self, producer, grade=None):
        c, ws = self.make_client(), Socket()
        def submit(path, payload):
            self.assertTrue(ws.ready, 'submit happened before subscription fence')
            self.assertEqual(path, '/api/v1/chat/submit')
            producer(ws, payload)
            return dict(accepted=True, **{k: payload[k] for k in (
                'turn_id', 'session_id', 'conversation_id')})
        c.post = Mock(side_effect=submit)
        with patch.object(transport, 'websocket_connect', return_value=ws) as connect:
            if grade is None:
                result = c.chat('multiply', 'session-fixture', 'chat')
            else:
                result = async_wait.run_async_aware(c, '/fixture-home', 'C', 'N', 'chat',
                                                    'multiply', grade, session='session-fixture')
        self.assertTrue(ws.closed)
        self.assertEqual(c.post.call_count, 1)
        self.assertEqual(connect.call_count, 1)
        return c, result

    def test_default_and_explicit_legacy(self):
        self.assertEqual(client.parse_args([]).transport, 'async')
        self.assertEqual(client.parse_args(['--transport', 'legacy']).transport, 'legacy')

    def test_parked_duplicate_then_final_not_ack(self):
        def producer(ws, p):
            parked = event(p, status='parked', reply='accepted task')
            ws.emit(parked)
            ws.emit(parked)
            ws.emit(event(p))
            ws.emit(event(p))
        c, reply = self.exercise(producer)
        self.assertEqual(reply, '391')
        self.assertTrue(c.last_terminal['turn_id'].startswith('turn-'))

    def test_terminal_before_ack_and_grading_context(self):
        def producer(ws, p):
            ws.emit(event(p))
            self.assertTrue(ws.terminal_read.wait(1), "terminal was not consumed before ack")
        grade = Mock(return_value=('PASS', ''))
        c, result = self.exercise(producer, grade)
        self.assertEqual(result[0], 'PASS')
        reply, ctx = grade.call_args.args
        self.assertEqual(reply, '391')
        self.assertEqual(ctx, dict(c.last_terminal, home='/fixture-home'))
        self.assertNotIn('legacy', ctx)

    def test_other_identities_ignored(self):
        def producer(ws, p):
            for key in ('turn_id', 'session_id', 'conversation_id'):
                ws.emit(event(p, **{key: 'unrelated'}, reply='WRONG'))
            ws.emit(event(p))
        _, reply = self.exercise(producer)
        self.assertEqual(reply, '391')

    def test_failed_and_server_timeout_never_grade(self):
        for status, verdict in [('failed', 'FAIL'), ('timeout', 'TIMEOUT')]:
            grade = Mock()
            _, result = self.exercise(lambda ws, p: ws.emit(event(p, status=status)), grade)
            self.assertEqual(result[0], verdict)
            grade.assert_not_called()

    def test_missing_terminal_times_out_without_grading(self):
        grade = Mock()
        _, result = self.exercise(lambda ws, p: ws.emit(event(p, status='parked')), grade)
        self.assertEqual(result[0], 'TIMEOUT')
        grade.assert_not_called()

    def test_disconnect_is_explicit_missing_events_no_reconnect_resubmit(self):
        c, ws = self.make_client(), Socket()
        def submit(path, p):
            ws.emit(ConnectionError('fixture drop'))
            return dict(accepted=True, **{k: p[k] for k in ('turn_id','session_id','conversation_id')})
        c.post = Mock(side_effect=submit)
        with patch.object(transport, 'websocket_connect', return_value=ws) as connect:
            with self.assertRaisesRegex(transport.AsyncTransportError, 'events may be missing'):
                c.chat('multiply', 'session-fixture')
        self.assertEqual(connect.call_count, 1)
        self.assertEqual(c.post.call_count, 1)
        self.assertIsNone(c.last_terminal)

    def test_mismatched_ack_fails_even_after_terminal(self):
        c, ws = self.make_client(), Socket()
        def submit(path, p):
            ws.emit(event(p))
            return dict(accepted=True, turn_id='wrong', session_id=p['session_id'],
                        conversation_id=p['conversation_id'])
        c.post = Mock(side_effect=submit)
        with patch.object(transport, 'websocket_connect', return_value=ws):
            with self.assertRaisesRegex(transport.AsyncTransportError, 'mismatches turn_id'):
                c.chat('multiply', 'session-fixture')
        self.assertEqual(c.post.call_count, 1)

    def test_progress_extends_inactivity_window(self):
        def producer(ws, p):
            # Total healthy duration exceeds timeout; correlated progress renews it.
            for delay in (0.04, 0.08, 0.12):
                ws.loop.call_soon_threadsafe(
                    ws.loop.call_later, delay, ws.emit,
                    event(p, source_topic='task.progress'))
            ws.loop.call_soon_threadsafe(ws.loop.call_later, 0.16, ws.emit, event(p))
        _, reply = self.exercise(producer)
        self.assertEqual(reply, '391')

    def test_missing_dependency_is_actionable(self):
        original = builtins.__import__
        def missing(name, *args, **kwargs):
            if name.startswith('websockets'):
                raise ImportError('fixture')
            return original(name, *args, **kwargs)
        with patch('builtins.__import__', side_effect=missing):
            with self.assertRaisesRegex(transport.AsyncTransportError, 'optional.*websockets'):
                transport.websocket_connect('ws://fixture', 'fixture', None, 1)

    def test_legacy_context_has_no_manufactured_terminal(self):
        c = Mock(transport='legacy')
        c.chat.return_value = '391'
        grade = Mock(return_value=('PASS', ''))
        async_wait.run_async_aware(c, '/home', 'C', 'N', 'chat', 'multiply', grade)
        self.assertEqual(grade.call_args.args, ('391', {'home': '/home', 'legacy': True}))


if __name__ == '__main__':
    unittest.main()
