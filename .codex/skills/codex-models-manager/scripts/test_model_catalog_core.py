import copy
import unittest

import model_catalog_core as core


class GPTTemplateTests(unittest.TestCase):
    def setUp(self):
        self.gpt = {
            "slug": "gpt-6.1-sol",
            "context_window": 500000,
            "max_context_window": 872000,
            "auto_compact_token_limit": None,
            "supported_reasoning_levels": [{"effort": "high"}, {"effort": "ultra"}],
            "tool_mode": "code_mode_only",
            "use_responses_lite": True,
            "model_messages": {"instructions_template": "existing prompt", "multi_agent": {"v": 2}},
        }
        self.other = {"slug": "deepseek-example", "context_window": 1000000}
        self.catalog = {"models": [self.other, self.gpt]}

    def test_gpt_add_clones_gpt_capabilities_without_mutating_template(self):
        original = copy.deepcopy(self.gpt)
        added = core.add_model(self.catalog, {"slug": "gpt-new", "display_name": "New GPT"})
        self.assertEqual(added["context_window"], 500000)
        self.assertEqual(added["max_context_window"], 872000)
        self.assertIsNone(added["auto_compact_token_limit"])
        self.assertNotIn("max_output_tokens", added)
        self.assertEqual(added["supported_reasoning_levels"], original["supported_reasoning_levels"])
        self.assertEqual(added["tool_mode"], "code_mode_only")
        self.assertTrue(added["use_responses_lite"])
        self.assertEqual(added["model_messages"]["instructions_template"], core.DEFAULT_TEMPLATE_PLACEHOLDER)
        added["model_messages"]["multi_agent"]["v"] = 3
        self.assertEqual(self.gpt, original)

    def test_explicit_template_and_fields_win(self):
        added = core.add_model(
            self.catalog, {"slug": "gpt-new", "context_window": 123000}, self.other["slug"]
        )
        self.assertEqual(added["context_window"], 123000)
        self.assertNotIn("tool_mode", added)

    def test_any_gpt_template_is_used_when_preferred_entries_are_missing(self):
        self.gpt["slug"] = "gpt-custom"
        self.assertEqual(core.select_template_slug(self.catalog, "GPT-NEW"), "gpt-custom")

    def test_no_gpt_template_fails_without_changing_catalog(self):
        for models in ([self.other], []):
            with self.subTest(models=models):
                catalog = {"models": models}
                original = copy.deepcopy(catalog)
                with self.assertRaisesRegex(ValueError, "No GPT template"):
                    core.add_model(catalog, {"slug": "gpt-new"})
                self.assertEqual(catalog, original)

    def test_non_gpt_default_still_uses_first_model(self):
        added = core.add_model(self.catalog, {"slug": "other-model"})
        self.assertEqual(added["context_window"], self.other["context_window"])

    def test_missing_explicit_template_is_reported(self):
        with self.assertRaisesRegex(ValueError, "not found"):
            core.add_model(self.catalog, {"slug": "gpt-new"}, "missing-model")


if __name__ == "__main__":
    unittest.main()
