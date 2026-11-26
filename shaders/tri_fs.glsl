#version 120
varying vec3 vBary;
varying vec4 vColor;
varying vec2 vClipMin;
varying vec2 vClipMax;
varying vec2 vFragPos; // from the geometry, not gl_FragCoord (which lags a resize)
uniform float uClipEnabled;
void main() {
  // Per-vertex clip (GL window coords, bottom-left origin): lets batches
  // span clip changes without flushing.
  if (uClipEnabled > 0.5 &&
      (vFragPos.x < vClipMin.x || vFragPos.x >= vClipMax.x ||
       vFragPos.y < vClipMin.y || vFragPos.y >= vClipMax.y)) {
    discard;
  }
  // Pixel-distance AA on barycentric coordinates; per-vertex color removes
  // the need to flush the batch on every color change.
  vec3 fw = fwidth(vBary);
  vec3 px = vBary / max(fw, vec3(1e-6));
  vec3 a = clamp(px + 0.5, 0.0, 1.0);
  float alpha = min(min(a.x, a.y), a.z);
  gl_FragColor = vec4(vColor.rgb, vColor.a * alpha);
}
