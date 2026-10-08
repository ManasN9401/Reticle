from pathlib import Path
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cmd/forge/compiler/lib"))
import web_tools

THREE_CDN = '<script src="https://cdnjs.cloudflare.com/ajax/libs/three.js/r128/three.min.js"></script>'


class Site:
    def __init__(self, files):
        self.temp = tempfile.TemporaryDirectory()
        self.workspace = self.temp.name
        for name, content in files.items():
            target = Path(self.workspace, "src", name)
            target.parent.mkdir(parents=True, exist_ok=True)
            if isinstance(content, bytes):
                target.write_bytes(content)
            else:
                target.write_text(content, encoding="utf-8")

    def check(self, path=None):
        return web_tools.run_check(self.workspace, path)


class WebChecks(unittest.TestCase):
    def site(self, files):
        site = Site(files)
        self.addCleanup(site.temp.cleanup)
        return site

    def test_a_clean_page_has_no_blocking_problems(self):
        site = self.site({
            "index.html": f'<link rel="stylesheet" href="style.css">{THREE_CDN}<canvas id="stage"></canvas><script src="main.js"></script>',
            "style.css": "body{margin:0}",
            "main.js": "const renderer = new THREE.WebGLRenderer({ canvas: document.getElementById('stage') })",
        })
        report, blocking = site.check()
        self.assertEqual(blocking, 0, report)
        self.assertIn("No blocking problems found", report)

    def test_a_classic_script_using_import_is_the_failure_seen_in_a_real_run(self):
        site = self.site({
            "index.html": f'{THREE_CDN}<div id="gallery-canvas"></div><script src="script.js"></script>',
            "script.js": "import * as THREE from 'three';\nimport gsap from 'gsap';\nconsole.log(THREE)",
        })
        report, blocking = site.check()
        self.assertEqual(blocking, 1, report)
        self.assertIn("Cannot use import statement outside a module", report)
        self.assertIn("script.js", report)

    def test_exports_and_inline_classic_scripts_are_caught_too(self):
        site = self.site({
            "a.html": "<script src=\"a.js\"></script>",
            "a.js": "export function run() {}",
            "b.html": "<script>\nimport x from './x.js'\n</script>",
        })
        self.assertEqual(site.check("a.html")[1], 1)
        report, blocking = site.check("b.html")
        self.assertEqual(blocking, 1, report)
        self.assertIn("inline script", report)

    def test_dynamic_import_and_commented_imports_are_fine_in_a_classic_script(self):
        site = self.site({
            "index.html": '<script src="app.js"></script>',
            "app.js": "// import x from 'y'\nconst load = () => import('./lazy.js')\n",
            "lazy.js": "export default 1",
        })
        self.assertEqual(site.check()[1], 0, site.check()[0])

    def test_module_scripts_need_an_import_map_for_bare_specifiers(self):
        files = {
            "index.html": '<script type="module" src="main.js"></script>',
            "main.js": "import * as THREE from 'three'\nimport { helper } from './helper.js'",
            "helper.js": "import gsap from 'gsap'\nexport const helper = 1",
        }
        report, blocking = self.site(files).check()
        self.assertEqual(blocking, 2, report)
        self.assertIn("'three'", report)
        self.assertIn("'gsap'", report)
        files["index.html"] = (
            '<script type="importmap">{"imports":{"three":"https://cdn.jsdelivr.net/npm/three@0.160/build/three.module.js",'
            '"gsap":"https://cdn.jsdelivr.net/npm/gsap@3.12/index.js"}}</script>' + files["index.html"]
        )
        self.assertEqual(self.site(files).check()[1], 0)

    def test_an_import_map_prefix_key_covers_subpaths(self):
        site = self.site({
            "index.html": '<script type="importmap">{"imports":{"three/":"https://cdn.example/three/"}}</script><script type="module" src="m.js"></script>',
            "m.js": "import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'",
        })
        self.assertEqual(site.check()[1], 0, site.check()[0])

    def test_missing_local_files_are_reported(self):
        site = self.site({"index.html": '<link rel="stylesheet" href="css/missing.css"><img src="img/none.png"><script src="gone.js"></script>'})
        report, blocking = site.check()
        self.assertEqual(blocking, 3, report)

    def test_remote_and_data_references_are_not_checked(self):
        site = self.site({"index.html": '<img src="https://example.com/a.png"><img src="data:image/png;base64,AAAA"><img src="//cdn.example/b.png">'})
        self.assertEqual(site.check()[1], 0)

    def test_a_renderer_canvas_that_is_a_div_is_reported(self):
        site = self.site({
            "index.html": f'{THREE_CDN}<div id="gallery-canvas"></div><script src="s.js"></script>',
            "s.js": "const r = new THREE.WebGLRenderer({ canvas: document.getElementById('gallery-canvas'), antialias: true })",
        })
        report, blocking = site.check()
        self.assertEqual(blocking, 1, report)
        self.assertIn("must be a <canvas>", report)

    def test_unreferenced_images_and_unknown_ids_are_warnings_only(self):
        site = self.site({
            "index.html": '<script src="a.js"></script><img src="used.png">',
            "a.js": "document.getElementById('ghost')",
            "used.png": b"\x89PNG", "unused.png": b"\x89PNG",
        })
        report, blocking = site.check()
        self.assertEqual(blocking, 0, report)
        self.assertIn("unused.png exists but no page", report)
        self.assertNotIn("used.png exists", report.replace("unused.png exists", ""))
        self.assertIn("looks up #ghost", report)

    def test_references_resolve_from_the_page_not_the_workspace(self):
        # write_file("src/index.html") lands at src/src/index.html; its relative links are relative to it.
        site = self.site({"src/index.html": '<link rel="stylesheet" href="style.css">', "src/style.css": "a{}"})
        report, blocking = site.check()
        self.assertEqual(blocking, 0, report)
        self.assertIn("src/index.html", report)

    def test_no_page_is_not_a_problem_and_a_named_page_is_found(self):
        self.assertEqual(self.site({"notes.md": "x"}).check()[1], 0)
        self.assertIn("No .html file", self.site({"notes.md": "x"}).check()[0])
        site = self.site({"sub/p.html": "<p>x</p>"})
        self.assertIn("sub/p.html", site.check("sub/p.html")[0])

    @unittest.skipUnless(shutil.which("node"), "node is not installed")
    def test_a_real_syntax_error_is_reported_when_node_is_available(self):
        site = self.site({"index.html": '<script src="a.js"></script>', "a.js": "function ( {"})
        report, blocking = site.check()
        self.assertGreaterEqual(blocking, 1, report)
        self.assertIn("syntax error", report)


if __name__ == "__main__":
    unittest.main()
