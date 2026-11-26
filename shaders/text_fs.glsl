#version 120
uniform sampler2D uTexture;
uniform int uIsImage;
varying vec2 vUv;
varying vec4 vColor;
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
  vec4 t = texture2D(uTexture, vUv);
  if (uIsImage == 0) {
    // Glyph atlas stores per-channel coverage: LCD subpixel masks
    // (R/G/B differ) or replicated grayscale. Output PREMULTIPLIED
    // per channel; blend (ONE, ONE_MINUS_SRC_COLOR) composites each
    // subpixel separately. Framebuffer alpha uses the average coverage
    // so the window compositor sees a sane opacity.
    vec3 m = t.rgb;
    float a = (m.r + m.g + m.b) / 3.0 * vColor.a;
    gl_FragColor = vec4(vColor.rgb * m * vColor.a, a);
  } else {
    // Image textures are premultiplied; scaling by vColor keeps them so.
    gl_FragColor = t * vColor;
  }
}
