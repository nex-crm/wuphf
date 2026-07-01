#!/usr/bin/env python3
"""Unit tests for the cua runner scripts — stdlib unittest, no dependencies.

Run:  cd runner && python3 -m unittest test_runners -v

Covers the security-critical pure logic (secret redaction, menu-bar exclusion,
the press_key allowlist) and the observe snapshot/diff, by mocking the cua-driver
call so nothing real is driven.
"""

import unittest
from unittest import mock

import cua_common
import cua_exec
import cua_observe


class TestRedaction(unittest.TestCase):
    def test_secret_labels_are_redacted(self):
        label, secure = cua_common.redact_label("AXTextField", "Password")
        self.assertTrue(secure)
        self.assertIn("redacted", label)

    def test_secure_field_redacted_even_without_label(self):
        label, secure = cua_common.redact_label("AXSecureTextField", "")
        self.assertTrue(secure)

    def test_ordinary_label_passes_through(self):
        label, secure = cua_common.redact_label("AXTextField", "Email address")
        self.assertFalse(secure)
        self.assertEqual(label, "Email address")


class TestKeyAllowlist(unittest.TestCase):
    def test_nav_keys_allowed(self):
        self.assertEqual(cua_exec.safe_key("Enter"), "Enter")
        self.assertEqual(cua_exec.safe_key("ArrowDown"), "ArrowDown")

    def test_modifiers_and_chars_rejected(self):
        self.assertIsNone(cua_exec.safe_key("cmd+q"))
        self.assertIsNone(cua_exec.safe_key("q"))
        self.assertIsNone(cua_exec.safe_key("Enter Enter"))


class TestExecSnapshot(unittest.TestCase):
    def test_excludes_menu_bar_and_redacts(self):
        fake = {
            "elements": [
                {"element_index": 1, "role": "AXButton", "label": "Send"},
                {"element_index": 2, "role": "AXMenuBarItem", "label": "File"},
                {"element_index": 3, "role": "AXSecureTextField", "label": "Password"},
            ]
        }
        with mock.patch.object(cua_exec, "cua", return_value=fake):
            elements, err = cua_exec.snapshot(1, 1)
        self.assertEqual(err, "")
        labels = [e["label"] for e in elements]
        self.assertIn("Send", labels)
        self.assertNotIn("File", labels)  # macOS menu bar never reaches the model
        self.assertTrue(any("redacted" in l for l in labels))


class TestObserve(unittest.TestCase):
    def test_components_excludes_menu_bar_and_redacts(self):
        fake = {
            "elements": [
                {"role": "AXButton", "label": "Approve"},
                {"role": "AXMenuItem", "label": "Quit"},
                {"role": "AXTextField", "label": "API key"},
            ]
        }
        with mock.patch.object(cua_observe, "cua", return_value=fake):
            comps = cua_observe.components(1, 1)
        labels = [c["label"] for c in comps]
        self.assertIn("Approve", labels)
        self.assertNotIn("Quit", labels)
        self.assertTrue(any("redacted" in l for l in labels))

    def test_snapshot_skips_page_reads_for_non_browser(self):
        # A non-browser frontmost app: components captured, no text_excerpt.
        with (
            mock.patch.object(cua_observe, "frontmost_app_name", return_value="Slack"),
            mock.patch.object(cua_observe, "find_window", return_value=(1, 2, "Slack")),
            mock.patch.object(cua_observe, "components", return_value=[{"role": "Button", "label": "Send"}]),
            mock.patch.object(cua_observe, "visible_text") as text,
        ):
            snap = cua_observe.snapshot(0)
        self.assertEqual(snap["app"], "Slack")
        self.assertNotIn("text_excerpt", snap)
        text.assert_not_called()

    def test_snapshot_reads_text_for_browser(self):
        with (
            mock.patch.object(cua_observe, "frontmost_app_name", return_value="Google Chrome"),
            mock.patch.object(cua_observe, "find_window", return_value=(1, 2, "HubSpot")),
            mock.patch.object(cua_observe, "components", return_value=[]),
            mock.patch.object(cua_observe, "visible_text", return_value="Acme Robotics company record"),
        ):
            snap = cua_observe.snapshot(0)
        self.assertEqual(snap["text_excerpt"], "Acme Robotics company record")


class TestReplay(unittest.TestCase):
    def test_find_element_exact_fuzzy_miss(self):
        els = [
            {"i": 1, "role": "Button", "label": "Search"},
            {"i": 2, "role": "TextField", "label": "Company name"},
        ]
        self.assertEqual(cua_exec.find_element(els, "Button", "Search")["i"], 1)
        self.assertEqual(cua_exec.find_element(els, "TextField", "Company")["i"], 2)
        self.assertIsNone(cua_exec.find_element(els, "Button", "Nope"))

    def test_replay_matches_without_a_model_call(self):
        steps = [
            {"action": "click", "role": "Button", "label": "Search"},
            {"action": "type", "role": "TextField", "label": "Company", "text": "Acme"},
            {"action": "press_key", "key": "Enter"},
        ]
        elements = [
            {"i": 5, "role": "Button", "label": "Search"},
            {"i": 6, "role": "TextField", "label": "Company"},
        ]
        events = []
        with (
            mock.patch.object(cua_exec, "find_window", return_value=(1, 2, "t")),
            mock.patch.object(cua_exec, "snapshot", return_value=(elements, "")),
            mock.patch.object(cua_exec, "cua"),
            mock.patch.object(cua_exec, "plan") as plan_mock,
            mock.patch.object(cua_exec, "emit", side_effect=events.append),
        ):
            cua_exec.replay(steps, "goal", "Google Chrome", "key")
        plan_mock.assert_not_called()  # deterministic — every step matched
        actions = [e for e in events if e.get("type") == "action"]
        self.assertEqual(len(actions), 3)
        self.assertTrue(all(a.get("replayed") for a in actions))
        self.assertTrue(any(e.get("type") == "trajectory" for e in events))

    def test_live_run_records_a_trajectory(self):
        # A click then done → the run emits a trajectory with the clicked element's
        # stable identity (so a later run can replay it).
        plan_seq = [
            {"choices": [{"message": {"tool_calls": [{"id": "1", "function": {"name": "click", "arguments": '{"i":5,"reason":"x"}'}}]}}]},
            {"choices": [{"message": {"tool_calls": [{"id": "2", "function": {"name": "done", "arguments": '{"result":"ok"}'}}]}}]},
        ]
        events = []
        with (
            mock.patch.object(cua_exec, "find_window", return_value=(1, 2, "t")),
            mock.patch.object(cua_exec, "snapshot", return_value=([{"i": 5, "role": "Button", "label": "Go"}], "")),
            mock.patch.object(cua_exec, "cua"),
            mock.patch.object(cua_exec, "plan", side_effect=plan_seq),
            mock.patch.object(cua_exec, "emit", side_effect=events.append),
        ):
            cua_exec.run("do it", "Google Chrome", "key")
        traj = next(e for e in events if e.get("type") == "trajectory")
        self.assertEqual(traj["steps"], [{"action": "click", "role": "Button", "label": "Go"}])

    def test_replay_heals_when_element_is_gone(self):
        steps = [{"action": "click", "role": "Button", "label": "Gone"}]
        elements = [{"i": 9, "role": "Button", "label": "Renamed"}]
        events = []
        heal_resp = {
            "choices": [
                {"message": {"tool_calls": [{"function": {"name": "click", "arguments": '{"i":9,"reason":"x"}'}}]}}
            ]
        }
        with (
            mock.patch.object(cua_exec, "find_window", return_value=(1, 2, "t")),
            mock.patch.object(cua_exec, "snapshot", return_value=(elements, "")),
            mock.patch.object(cua_exec, "cua"),
            mock.patch.object(cua_exec, "plan", return_value=heal_resp),
            mock.patch.object(cua_exec, "emit", side_effect=events.append),
        ):
            cua_exec.replay(steps, "goal", "Google Chrome", "key")
        actions = [e for e in events if e.get("type") == "action"]
        self.assertEqual(len(actions), 1)
        self.assertTrue(actions[0].get("healed"))
        # The healed trajectory records the NEW element's stable identity.
        traj = next(e for e in events if e.get("type") == "trajectory")
        self.assertEqual(traj["steps"][0]["label"], "Renamed")


class TestSendGating(unittest.TestCase):
    def test_needs_approval(self):
        self.assertTrue(cua_common.needs_approval("Send"))
        self.assertTrue(cua_common.needs_approval("Post to channel"))
        self.assertTrue(cua_common.needs_approval("Reply all"))
        self.assertFalse(cua_common.needs_approval("Search"))
        self.assertFalse(cua_common.needs_approval("Open menu"))

    def _click_then_done(self):
        return [
            {"choices": [{"message": {"tool_calls": [{"id": "1", "function": {"name": "click", "arguments": '{"i":5,"reason":"send it"}'}}]}}]},
            {"choices": [{"message": {"tool_calls": [{"id": "2", "function": {"name": "done", "arguments": '{"result":"ok"}'}}]}}]},
        ]

    def test_run_skips_external_send_when_not_approved(self):
        events = []
        with (
            mock.patch.object(cua_exec, "find_window", return_value=(1, 2, "t")),
            mock.patch.object(cua_exec, "snapshot", return_value=([{"i": 5, "role": "Button", "label": "Send"}], "")),
            mock.patch.object(cua_exec, "cua") as cua_mock,
            mock.patch.object(cua_exec, "await_approval", return_value=False),
            mock.patch.object(cua_exec, "plan", side_effect=self._click_then_done()),
            mock.patch.object(cua_exec, "emit", side_effect=events.append),
        ):
            cua_exec.run("send it", "Google Chrome", "key")
        clicks = [c for c in cua_mock.call_args_list if c.args and c.args[0] == "click"]
        self.assertEqual(len(clicks), 0)  # the send was NOT auto-fired
        self.assertTrue(any(e.get("skipped") for e in events if e.get("type") == "action"))

    def test_healed_send_is_also_gated(self):
        # A recorded step whose element is gone heals onto a "Send" element — the
        # HEALED click must still be gated (regression: healing bypassed the gate).
        steps = [{"action": "click", "role": "Button", "label": "Gone"}]
        elements = [{"i": 9, "role": "Button", "label": "Send"}]
        heal_resp = {"choices": [{"message": {"tool_calls": [{"function": {"name": "click", "arguments": '{"i":9,"reason":"x"}'}}]}}]}
        events = []
        with (
            mock.patch.object(cua_exec, "find_window", return_value=(1, 2, "t")),
            mock.patch.object(cua_exec, "snapshot", return_value=(elements, "")),
            mock.patch.object(cua_exec, "cua") as cua_mock,
            mock.patch.object(cua_exec, "await_approval", return_value=False),
            mock.patch.object(cua_exec, "plan", return_value=heal_resp),
            mock.patch.object(cua_exec, "emit", side_effect=events.append),
        ):
            cua_exec.replay(steps, "g", "Google Chrome", "key")
        clicks = [c for c in cua_mock.call_args_list if c.args and c.args[0] == "click"]
        self.assertEqual(len(clicks), 0)  # healed send was gated + denied → not sent
        self.assertTrue(any(e.get("skipped") for e in events if e.get("type") == "action"))

    def test_run_sends_when_approved(self):
        events = []
        with (
            mock.patch.object(cua_exec, "find_window", return_value=(1, 2, "t")),
            mock.patch.object(cua_exec, "snapshot", return_value=([{"i": 5, "role": "Button", "label": "Send"}], "")),
            mock.patch.object(cua_exec, "cua") as cua_mock,
            mock.patch.object(cua_exec, "await_approval", return_value=True),
            mock.patch.object(cua_exec, "plan", side_effect=self._click_then_done()),
            mock.patch.object(cua_exec, "emit", side_effect=events.append),
        ):
            cua_exec.run("send it", "Google Chrome", "key")
        clicks = [c for c in cua_mock.call_args_list if c.args and c.args[0] == "click"]
        self.assertEqual(len(clicks), 1)  # approved → executed


if __name__ == "__main__":
    unittest.main()
