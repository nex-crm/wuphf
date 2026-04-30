"""Unit tests for scripts/skill_eval/eval.py — HOME isolation guard."""
import os
import sys
import unittest
from pathlib import Path

# Allow importing eval.py as a module from the same directory.
sys.path.insert(0, str(Path(__file__).parent))
import eval as skill_eval  # noqa: E402


class TestDevHome(unittest.TestCase):
    """_dev_home() must never produce a doubled path."""

    def setUp(self) -> None:
        self._original_home = os.environ.get("HOME", "")

    def test_plain_home_appends_suffix(self) -> None:
        """When HOME is a normal user home, suffix is appended."""
        os.environ["HOME"] = "/Users/testuser"
        result = skill_eval._dev_home()
        self.assertEqual(result, "/Users/testuser/.wuphf-dev-home")

    def test_already_dev_home_is_idempotent(self) -> None:
        """When HOME already ends with /.wuphf-dev-home, no doubling occurs."""
        os.environ["HOME"] = "/Users/testuser/.wuphf-dev-home"
        result = skill_eval._dev_home()
        self.assertEqual(result, "/Users/testuser/.wuphf-dev-home")

    def test_nested_doubling_is_prevented(self) -> None:
        """Calling _dev_home() twice returns the same value both times."""
        os.environ["HOME"] = "/Users/testuser"
        first = skill_eval._dev_home()
        os.environ["HOME"] = first  # simulate shell that already set HOME
        second = skill_eval._dev_home()
        self.assertEqual(first, second)

    def test_apply_home_isolation_sets_env(self) -> None:
        """apply_home_isolation() mutates os.environ['HOME'] correctly."""
        os.environ["HOME"] = "/Users/testuser"
        result = skill_eval.apply_home_isolation()
        self.assertEqual(result, "/Users/testuser/.wuphf-dev-home")
        self.assertEqual(os.environ["HOME"], "/Users/testuser/.wuphf-dev-home")

    def test_apply_home_isolation_idempotent(self) -> None:
        """Calling apply_home_isolation() when HOME is already isolated is safe."""
        os.environ["HOME"] = "/Users/testuser/.wuphf-dev-home"
        result = skill_eval.apply_home_isolation()
        self.assertEqual(result, "/Users/testuser/.wuphf-dev-home")
        self.assertEqual(os.environ["HOME"], "/Users/testuser/.wuphf-dev-home")

    def tearDown(self) -> None:
        os.environ["HOME"] = self._original_home


if __name__ == "__main__":
    unittest.main()
