import test from 'node:test'
import assert from 'node:assert/strict'
import { extensionOf, fileKindFor } from '../src/shared/fileKind'

test('image files are recognised by extension, case-insensitively', () => {
  for (const path of ['assets/gallery-01.png', 'a/b.JPG', 'x.jpeg', 'y.gif', 'z.webp', 'c.avif', 'logo.svg', 'favicon.ico', 'old.bmp', 'dir.v2/photo.PNG']) {
    assert.equal(fileKindFor(path), 'image', path)
  }
})

test('code, markup and unknown files are text', () => {
  for (const path of ['index.html', 'js/main.js', 'style.css', 'README.md', 'data.json', 'Dockerfile', 'noext', 'archive.tar.gz', 'png', 'a/png']) {
    assert.equal(fileKindFor(path), 'text', path)
  }
})

test('a dotfile has no extension and only the last segment counts', () => {
  assert.equal(extensionOf('.png'), '')
  assert.equal(extensionOf('folder.png/readme'), '')
  assert.equal(extensionOf('win\\path\\pic.GIF'), 'gif')
})

test('3D models and the other common raster formats get their own viewers', () => {
  for (const path of ['assets/models/frame.glb', 'scene.GLTF', 'dir/a.glb']) {
    assert.equal(fileKindFor(path), 'model', path)
  }
  for (const path of ['photo.jfif', 'anim.apng', 'tex/wall-marble-01.jpg', 'tex/wall.JPEG']) {
    assert.equal(fileKindFor(path), 'image', path)
  }
  // Other 3D formats the viewer cannot open stay text rather than opening as a broken viewport.
  for (const path of ['mesh.obj', 'scene.fbx', 'model.glb.md', 'glb']) {
    assert.equal(fileKindFor(path), 'text', path)
  }
})
