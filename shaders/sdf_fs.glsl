#version 120
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
varying vec4 vGradA;   // (kind, anchor x/y, radius or start angle)
varying vec2 vFragPos; // fragment position for clipping, from aPos/uScreenSize (GL window space)
varying vec4 vGradB;   // (linear end x/y, LUT row v, unused)
uniform float uClipEnabled;
// Convex path clip: up to sdfMaxClipPlanes half-planes, intersected. A pixel
// survives only if it is on the kept side of every plane in its range.
#define MAX_CLIP_PLANES 24
uniform vec3 uClipPlane[MAX_CLIP_PLANES];
uniform sampler2D uTex0;
uniform sampler2D uTex1;
uniform sampler2D uTex2;
uniform sampler2D uTex3;
uniform sampler2D uTex4;
uniform sampler2D uTex5;
uniform sampler2D uTex6;
uniform sampler2D uTex7;
uniform sampler2D uTex8;
uniform sampler2D uTex9;
uniform sampler2D uTex10;
uniform sampler2D uTex11;
uniform sampler2D uTex12;
uniform sampler2D uTex13;
uniform sampler2D uTex14;
uniform sampler2D uTex15;

// Position along the current primitive's gradient, in 0..1. The fragment
// position comes from vFragPos (the primitive's own geometry), not gl_FragCoord,
// which lags a vertical resize in this stack - the same source, for the same reason
// as the clip test above.
float gradientT() {
  float kind = vGradA.x;
  if (kind < 1.5) {
    // Linear: project onto start->end.
    vec2 d = vGradB.xy - vGradA.yz;
    float dd = dot(d, d);
    if (dd < 1e-6) return 0.0;
    return dot(vFragPos - vGradA.yz, d) / dd;
  }
  if (kind < 2.5) {
    // Radial: distance from the centre over the radius.
    return length(vFragPos - vGradA.yz) / max(vGradA.w, 1e-6);
  }
  // Conic: sweep around the centre from the start angle.
  vec2 d = vFragPos - vGradA.yz;
  float a = atan(d.y, d.x) - radians(vGradA.w);
  float t = a / 6.28318530718;
  return t - floor(t);
}

// GLSL 1.20 cannot index sampler arrays with a non-constant, so select
// the bound texture with a compile-time-unrollable branch chain.
vec4 sampleUnit(float unit, vec2 uv) {
  if (unit < 0.5) return texture2D(uTex0, uv);
  if (unit < 1.5) return texture2D(uTex1, uv);
  if (unit < 2.5) return texture2D(uTex2, uv);
  if (unit < 3.5) return texture2D(uTex3, uv);
  if (unit < 4.5) return texture2D(uTex4, uv);
  if (unit < 5.5) return texture2D(uTex5, uv);
  if (unit < 6.5) return texture2D(uTex6, uv);
  if (unit < 7.5) return texture2D(uTex7, uv);
  if (unit < 8.5) return texture2D(uTex8, uv);
  if (unit < 9.5) return texture2D(uTex9, uv);
  if (unit < 10.5) return texture2D(uTex10, uv);
  if (unit < 11.5) return texture2D(uTex11, uv);
  if (unit < 12.5) return texture2D(uTex12, uv);
  if (unit < 13.5) return texture2D(uTex13, uv);
  if (unit < 14.5) return texture2D(uTex14, uv);
  return texture2D(uTex15, uv);
}

// The primitive's base colour: the gradient ramp when it carries one (modulated
// by the paint alpha, which includes global alpha), otherwise vColor. Only the
// shape (0) and barycentric-triangle (3) modes carry a gradient; glyph and
// image quads sample their own texture.
void baseColor(out vec3 rgb, out float a) {
  rgb = vColor.rgb;
  a = vColor.a;
  if (vGradA.x > 0.5 && (vMode < 0.5 || (vMode > 2.5 && vMode < 3.5))) {
    vec4 g = sampleUnit(vTexUnit, vec2(clamp(gradientT(), 0.0, 1.0), vGradB.z));
    rgb = g.rgb;
    a = g.a * vColor.a;
  }
}

void main() {
  // MEUI_FRAGCOORD_DEBUG (uClipEnabled = 3.0): paint the two candidate fragment
  // positions for every fragment, no test - R = gl_FragCoord.y/1000, G =
  // vFragPos.y/1000. In a stuck frame R lagged by exactly the resize delta while
  // G was correct, which is what identified gl_FragCoord as the faulty source.
  if (uClipEnabled > 2.5 && uClipEnabled < 3.5) {
    gl_FragColor = vec4(gl_FragCoord.y / 1000.0, vFragPos.y / 1000.0, vClipRange.y / 10.0, 1.0);
    return;
  }
  // MEUI_CLIP_VERDICT (uClipEnabled = 4.0): evaluate the clip test and paint its
  // verdict (red = would discard, green = would pass) instead of acting on it.
  // This separates "the test rejects these fragments" from the values it reads,
  // which the value-painting mode cannot: it returns before the test.
  if (uClipEnabled > 3.5) {
    bool rej = (vFragPos.x < vClipMin.x || vFragPos.x >= vClipMax.x ||
                vFragPos.y < vClipMin.y || vFragPos.y >= vClipMax.y);
    gl_FragColor = rej ? vec4(1.0, 0.0, 0.0, 1.0) : vec4(0.0, 1.0, 0.0, 1.0);
    return;
  }
  // Per-vertex clip (GL window coords): batches span clip changes.
  if (uClipEnabled > 0.5) {
    if (vFragPos.x < vClipMin.x || vFragPos.x >= vClipMax.x ||
        vFragPos.y < vClipMin.y || vFragPos.y >= vClipMax.y) {
      // MEUI_CLIP_DEBUG sets uClipEnabled to 2.0: instead of discarding, paint
      // the values THIS fragment shader actually holds. The Go array and the
      // VBO have both been read and say the clip is current, so if the shader
      // rejects a fragment the disagreement is here: R = vClipMax.y/1000,
      // G = vClipMin.y/1000, B = vClipRange.y/10.
      if (uClipEnabled > 1.5) {
        gl_FragColor = vec4(vClipMax.y / 1000.0, vClipMin.y / 1000.0, vClipRange.y / 10.0, 1.0);
        return;
      }
      discard;
    }
    // Path clip planes, if any (count is 0 for the many primitives that only
    // carry the rect). The loop bound is constant so the array index stays a
    // uniform-derived expression the ES2 compiler can handle.
    for (int i = 0; i < MAX_CLIP_PLANES; i++) {
      if (float(i) >= vClipRange.y) break;
      vec3 pl = uClipPlane[int(vClipRange.x) + i];
      if (dot(pl.xy, vFragPos) + pl.z < 0.0) {
        discard;
      }
    }
  }
  if (vMode > 0.5) {
    if (vMode < 1.5) {
      vec4 t = sampleUnit(vTexUnit, vUv);
      // Glyph atlas stores per-channel coverage: LCD subpixel masks
      // (R/G/B differ) or replicated grayscale. Output PREMULTIPLIED
      // so the batch uses one blend state for shapes, text and images.
      vec3 m = t.rgb;
      float a = (m.r + m.g + m.b) / 3.0 * vColor.a;
      gl_FragColor = vec4(vColor.rgb * m * vColor.a, a);
      return;
    }
    if (vMode < 2.5) {
      vec4 t = sampleUnit(vTexUnit, vUv);
      // Image textures are premultiplied; scaling by vColor keeps them so.
      gl_FragColor = t * vColor;
      return;
    }
    // Barycentric triangle (mode 3): pixel-distance AA on barycentric
    // coords, same math as the dedicated tri pipeline. Packed as
    // aLocal=(b1,b2), aHalf.x=b3.
    vec3 bary = vec3(vLocal, vHalf.x);
    vec3 fw = fwidth(bary);
    vec3 px = bary / max(fw, vec3(1e-6));
    // Antialiasing off: cover a fragment exactly when it is inside the
    // triangle (all barycentric coords non-negative), with no ramp.
    vec3 ac = vAA > 0.5 ? clamp(px + 0.5, 0.0, 1.0) : step(vec3(0.0), bary);
    vec3 rgb; float colA;
    baseColor(rgb, colA);
    float ta = min(min(ac.x, ac.y), ac.z) * colA;
    gl_FragColor = vec4(rgb * ta, ta);
    return;
  }
  // Rounded-box signed distance (covers rect @ r=0, circle @ r=half, oval
  // approx @ r=min(half)). Analytic AA via screen-space derivative — no
  // tessellation, no stencil, no MSAA needed, seam-free by construction.
  vec2 q = abs(vLocal) - (vHalf - vec2(vRadius));
  float d = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - vRadius;
  // The edge ramp is the screen-space derivative of the distance field.
  // Paint.Antialias = false collapses it to the isocontour, giving the hard
  // pixel-aligned edge a caller asking for no AA expects.
  float aa = vAA > 0.5 ? max(fwidth(d), 1e-5) : 1e-5;
  float alpha;
  if (vStyle < 0.5) {
    // Fill
    alpha = clamp(0.5 - d / aa, 0.0, 1.0);
  } else if (vStyle < 1.5) {
    // Stroke as an INSIDE band of width sw. A centered band splits a
    // 1px border across two pixels at half coverage (reads as a soft
    // 2px line on every bordered widget); the inside band keeps
    // pixel-aligned borders one crisp pixel wide, like every desktop
    // toolkit draws them.
    float sw = max(vStrokeW, 1.0);
    float outer = clamp(0.5 - d / aa, 0.0, 1.0);
    float inner = clamp(0.5 - (d + sw) / aa, 0.0, 1.0);
    alpha = outer - inner;
  } else if (vStyle < 2.5) {
    // Fill + Stroke: interior up to the inner edge, plus the ring.
    float sw = max(vStrokeW, 1.0);
    float outer = clamp(0.5 - d / aa, 0.0, 1.0);
    float inner = clamp(0.5 - (d + sw) / aa, 0.0, 1.0);
    alpha = max(outer - inner, inner);
  } else {
    // Inverse fill (style 3): coverage OUTSIDE the shape. Used by the
    // rounded-window erase pass — drawn with (ZERO, ONE_MINUS_SRC_ALPHA)
    // it zeroes the framebuffer alpha outside the window plate so the
    // compositor shows the desktop through the corners.
    alpha = clamp(0.5 + d / aa, 0.0, 1.0);
  }
  // Premultiplied output: the unified batch composites with
  // (ONE, ONE_MINUS_SRC_ALPHA) for every primitive kind.
  vec3 rgb; float colA;
  baseColor(rgb, colA);
  float a = colA * alpha;
  gl_FragColor = vec4(rgb * a, a);
}
