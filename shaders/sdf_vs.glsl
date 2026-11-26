#version 120
attribute vec2 aPos;      // screen-space position (px)
attribute vec2 aLocal;    // position relative to shape center (px)
attribute vec2 aHalf;     // half size (px)
attribute float aRadius;  // corner radius (px, 0 = sharp box)
attribute float aStyle;   // 0 = fill, 1 = stroke, 2 = fill+stroke
attribute float aStrokeW; // stroke width (px)
attribute vec4 aColor;
attribute vec2 aClipMin;
attribute vec2 aClipMax;
attribute vec2 aUv;       // texture coords (mode 1/2)
attribute float aMode;    // 0 = SDF shape, 1 = glyph, 2 = image
attribute float aTexUnit; // sampler unit index (mode 1/2)
attribute float aAA;      // 1 = analytic antialiasing, 0 = hard edges
attribute vec2 aClipRange; // (base, count) into the uClipPlane table
attribute vec4 aGradA;    // (kind, anchor x/y, radius or start angle)
attribute vec4 aGradB;    // (linear end x/y, LUT row v, unused)
uniform vec2 uScreenSize;
varying vec2 vLocal;
varying vec2 vHalf;
varying float vRadius;
varying float vStyle;
varying float vStrokeW;
varying vec4 vColor;
varying vec2 vClipMin;
varying vec2 vClipMax;
varying vec2 vUv;
varying float vMode;
varying float vTexUnit;
varying float vAA;
varying vec2 vClipRange;
varying vec4 vGradA;
varying vec4 vGradB;
varying vec2 vFragPos;
void main() {
  // The fragment's position for CLIPPING, derived from the same aPos and the
  // same uScreenSize the geometry is projected with, in the same GL window space
  // the clip rects are stamped in. gl_FragCoord was the only other source: after
  // a vertical resize it reported a y offset of exactly the resize delta in this
  // stack (measured by painting it), so a clip test driven by it cut the frame at
  // the previous height. Position for clipping now comes from the geometry, which
  // cannot disagree with it.
  vFragPos = vec2(aPos.x, uScreenSize.y - aPos.y);
  vClipMin = aClipMin;
  vClipMax = aClipMax;
  vLocal = aLocal;
  vHalf = aHalf;
  vRadius = aRadius;
  vStyle = aStyle;
  vStrokeW = aStrokeW;
  vColor = aColor;
  vUv = aUv;
  vMode = aMode;
  vTexUnit = aTexUnit;
  vAA = aAA;
  vClipRange = aClipRange;
  vGradA = aGradA;
  vGradB = aGradB;
  vec2 ndc = aPos / uScreenSize;
  gl_Position = vec4((ndc.x - 0.5) * 2.0, (0.5 - ndc.y) * 2.0, 0.0, 1.0);
}
