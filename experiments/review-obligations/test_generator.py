"""Compiler boundary tests; protocol properties live in Oracle.tla."""

import copy
import json
import unittest

import check
import generate


class GeneratorTests(unittest.TestCase):
    def setUp(self):
        self.spec = generate.load(generate.ROOT / "transitions.json")

    def test_reproducible_outputs(self):
        self.assertEqual(generate.tla(self.spec), (generate.ROOT / "Generated.tla").read_text())
        self.assertEqual(generate.go(self.spec), (generate.ROOT / "generated.go").read_text())

    def test_unsupported_expression_rejected(self):
        for expr in [["execute", "anything"], ["+", 1], ["+", ["=", 1, 1], 1],
                     ["if", 1, 1, 0], "missingField", True, 99]:
            with self.subTest(expr=expr):
                spec = copy.deepcopy(self.spec)
                spec["actions"]["reset"]["set"]["cycle"] = expr
                with self.assertRaises(ValueError):
                    generate.validate(spec)

    def test_unknown_assignments_and_sections_rejected(self):
        for change in [lambda s: s.update(extra="ignored"),
                       lambda s: s["actions"]["reset"]["set"].update(unknown=1),
                       lambda s: s["actions"]["reset"].update(effect="ignored"),
                       lambda s: s["initial"].update(Mutation=0)]:
            with self.subTest(change=change):
                spec = copy.deepcopy(self.spec)
                change(spec)
                with self.assertRaises(ValueError):
                    generate.validate(spec)

    def test_duplicate_keys_rejected(self):
        with self.assertRaises(ValueError):
            json.loads('{"schema": 1, "schema": 2}', object_pairs_hook=generate.no_duplicates)

    def test_malformed_shapes_rejected(self):
        malformed = [None, [], {**self.spec, "schema": True},
                     {**self.spec, "actions": {"reset": []}},
                     {**self.spec, "actions": {"reset": {"guard": ["=", 1, 1], "set": {}}}}]
        for spec in malformed:
            with self.subTest(spec=spec), self.assertRaises(ValueError):
                generate.validate(spec)

    def test_reference_if_is_lazy(self):
        self.assertEqual(check.evaluate(["if", ["=", 1, 1], 2, "notEvaluated"], {}, {}), 2)


if __name__ == "__main__":
    unittest.main()
