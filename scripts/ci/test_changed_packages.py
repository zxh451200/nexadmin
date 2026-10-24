import unittest
from changed_packages import select, parse_diff
class ChangedPackagesTest(unittest.TestCase):
 def setUp(self):
  self.graph=[{"Dir":"/project/a","ImportPath":"m/a"},{"Dir":"/project/b","ImportPath":"m/b","TestImports":["m/a"]},{"Dir":"/project/c","ImportPath":"m/c","Imports":["m/b"]}]
 def choose(self,status,path,graph=True):return select([(status,[path])],self.graph if graph else None,"/project")
 def test_frontend(self):self.assertEqual(self.choose("M","web/src/a.ts")["packages"],[])
 def test_docs(self):self.assertEqual(self.choose("M","docs/a.md")["packages"],[])
 def test_reverse_test(self):self.assertEqual(self.choose("M","a/a.go")["packages"],["m/a","m/b","m/c"])
 def test_embed(self):self.assertEqual(self.choose("M","a/tpl/a.tmpl")["packages"],["m/a","m/b","m/c"])
 def test_delete(self):self.assertEqual(self.choose("D","a/a.go")["packages"],["./..."])
 def test_rename(self):self.assertEqual(parse_diff(b"R100\0a/a.go\0b/a.go\0"),[("R100",["a/a.go","b/a.go"])])
 def test_manifest(self):self.assertTrue(self.choose("M","go.sum")["licenses"])
 def test_graph_failure(self):self.assertEqual(self.choose("M","a/a.go",False)["packages"],["./..."])

 def test_ordinary(self):self.assertEqual(self.choose("M","c/c.go")["packages"],["m/c"])
 def test_rename_selection(self):self.assertEqual(select([("R100",["a/a.go","b/a.go"])],self.graph,"/project")["packages"],["./..."])
 def test_external_test_dependency(self):
  self.graph[1].pop("TestImports")
  self.graph[1]["XTestImports"]=["m/a"]
  self.assertEqual(self.choose("M","a/a.go")["packages"],["m/a","m/b","m/c"])
 def test_bad_graph(self):
  from changed_packages import read_graph
  with self.assertRaises(ValueError):read_graph("broken")

 def test_root_frontend_lockfiles(self):
  for path in ("npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock"):
   with self.subTest(path=path):
    result=self.choose("M",path)
    self.assertTrue(result["frontend"])
    self.assertTrue(result["licenses"])
    self.assertEqual(result["packages"],["./..."])
