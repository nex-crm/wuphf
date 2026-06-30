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


if __name__ == "__main__":
    unittest.main()
