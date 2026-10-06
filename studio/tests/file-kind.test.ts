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
