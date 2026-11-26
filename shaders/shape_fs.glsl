#version 120
varying vec2 vPos;
varying vec4 vColor;
varying float vShapeType;
varying vec2 vClipMin;
varying vec2 vClipMax;
varying vec2 vFragPos; // from the geometry, not gl_FragCoord (which lags a resize)
uniform float uClipEnabled;
void main() {
  // Per-vertex clip (GL window coords): batches span clip changes.
  if (uClipEnabled > 0.5 &&
      (vFragPos.x < vClipMin.x || vFragPos.x >= vClipMax.x ||
       vFragPos.y < vClipMin.y || vFragPos.y >= vClipMax.y)) {
    discard;
  }
  // Same SDF as the immediate rect shader (rect_fs): AA via fwidth.
  float edgeDist = 0.5 - max(abs(vPos.x), abs(vPos.y));
  if (vShapeType > 0.5) { edgeDist = 0.5 - length(vPos); }
  float d = fwidth(edgeDist) * 1.5;
  float coverage = smoothstep(-d, d, edgeDist);
  gl_FragColor = vec4(vColor.rgb, vColor.a * coverage);
}
