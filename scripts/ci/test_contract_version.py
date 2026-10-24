import unittest
from contract_version import check
class ContractVersionTest(unittest.TestCase):
 def test_changed_without_version(self):
  with self.assertRaises(ValueError):check(["core/testdata/contracts/module.golden"],"1.0.0","1.0.0")
 def test_changed_with_version(self):check(["core/testdata/contracts/module.golden"],"1.0.0","1.0.1")
 def test_implementation(self):check(["core/router.go"],"1.0.0","1.0.0")
