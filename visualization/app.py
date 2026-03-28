"""
Waze Traffic Simulation - 3D Desktop Visualization

Run:
    pip install -r requirements.txt
    python app.py

Controls:
    Left-drag : Pan camera
    Scroll    : Zoom in/out
    R         : Reset camera
    ESC       : Quit
"""

import sys
import math
import json
import threading
import time
import numpy as np
import pygame
from pygame.locals import *
import moderngl
import requests
import websocket

# ════════════════════════════════════════════════════
# Configuration
# ════════════════════════════════════════════════════
SERVER_URL = "http://localhost:8080"
WS_URL = "ws://localhost:8080/ws"
GRAPH_URL = f"{SERVER_URL}/api/graph"

CENTER_LON = 34.78
CENTER_LAT = 32.08
DEG_TO_M_LAT = 111320.0
DEG_TO_M_LON = DEG_TO_M_LAT * math.cos(math.radians(CENTER_LAT))

WIN_W, WIN_H = 1600, 900
FOV = 60.0
NEAR_CLIP = 20.0
FAR_CLIP = 50000.0
CAM_START_HEIGHT = 3000.0
CAM_TILT_DEG = 60.0
MAX_BUILDINGS = 6000

# ════════════════════════════════════════════════════
# GLSL Shaders
# ════════════════════════════════════════════════════

# --- Road / Ground shader (position + vertex color) ---
VERT_PC = """
#version 330 core
in vec3 in_position;
in vec3 in_color;
uniform mat4 mvp;
out vec3 v_color;
void main() {
    gl_Position = mvp * vec4(in_position, 1.0);
    v_color = in_color;
}
"""
FRAG_PC = """
#version 330 core
in vec3 v_color;
out vec4 fragColor;
void main() {
    fragColor = vec4(v_color, 1.0);
}
"""

# --- Building shader (position + normal + color, with diffuse lighting) ---
VERT_BLD = """
#version 330 core
in vec3 in_position;
in vec3 in_normal;
in vec3 in_color;
uniform mat4 mvp;
uniform vec3 light_dir;
out vec3 v_color;
out float v_light;
void main() {
    gl_Position = mvp * vec4(in_position, 1.0);
    v_color = in_color;
    float diff = max(dot(normalize(in_normal), normalize(light_dir)), 0.0);
    v_light = 0.45 + 0.55 * diff;
}
"""
FRAG_BLD = """
#version 330 core
in vec3 v_color;
in float v_light;
out vec4 fragColor;
void main() {
    fragColor = vec4(v_color * v_light, 1.0);
}
"""

# --- Car shader (GL_POINTS with circular shape) ---
VERT_CAR = """
#version 330 core
in vec3 in_position;
in vec3 in_color;
uniform mat4 mvp;
uniform float point_size;
out vec3 v_color;
void main() {
    gl_Position = mvp * vec4(in_position, 1.0);
    gl_PointSize = point_size;
    v_color = in_color;
}
"""
FRAG_CAR = """
#version 330 core
in vec3 v_color;
out vec4 fragColor;
void main() {
    vec2 c = gl_PointCoord - 0.5;
    float d = length(c);
    if (d > 0.5) discard;
    float rim = 1.0 - smoothstep(0.3, 0.5, d);
    fragColor = vec4(v_color * rim + vec3(1.0) * (1.0 - rim) * 0.3, 1.0);
}
"""

# --- HUD overlay shader (textured screen-space quad) ---
VERT_HUD = """
#version 330 core
in vec2 in_position;
in vec2 in_texcoord;
uniform vec4 rect;
out vec2 v_uv;
void main() {
    vec2 pos = rect.xy + in_position * rect.zw;
    gl_Position = vec4(pos, 0.0, 1.0);
    v_uv = in_texcoord;
}
"""
FRAG_HUD = """
#version 330 core
in vec2 v_uv;
uniform sampler2D tex;
out vec4 fragColor;
void main() {
    fragColor = texture(tex, v_uv);
}
"""

# ════════════════════════════════════════════════════
# Math Utilities
# ════════════════════════════════════════════════════

def perspective(fov, aspect, near, far):
    f = 1.0 / math.tan(math.radians(fov) / 2.0)
    m = np.zeros((4, 4), dtype='f4')
    m[0, 0] = f / aspect
    m[1, 1] = f
    m[2, 2] = (far + near) / (near - far)
    m[2, 3] = (2.0 * far * near) / (near - far)
    m[3, 2] = -1.0
    return m

def look_at(eye, target, up):
    eye = np.asarray(eye, dtype='f4')
    target = np.asarray(target, dtype='f4')
    up = np.asarray(up, dtype='f4')
    f = target - eye
    f /= np.linalg.norm(f)
    s = np.cross(f, up)
    s /= np.linalg.norm(s)
    u = np.cross(s, f)
    m = np.eye(4, dtype='f4')
    m[0, :3] = s
    m[1, :3] = u
    m[2, :3] = -f
    m[0, 3] = -s.dot(eye)
    m[1, 3] = -u.dot(eye)
    m[2, 3] = f.dot(eye)
    return m


def to_local(lon, lat):
    """Convert lon/lat to local meters (X right, Z down-screen = south)."""
    x = (lon - CENTER_LON) * DEG_TO_M_LON
    z = -(lat - CENTER_LAT) * DEG_TO_M_LAT
    return x, z


def speed_color(s):
    if s < 20:
        return (0.77, 0.13, 0.12)
    if s < 40:
        return (0.91, 0.44, 0.04)
    if s < 60:
        return (0.98, 0.67, 0.00)
    return (0.12, 0.56, 0.24)


def speed_color_vec(sl):
    """Vectorised speed→RGB for numpy arrays."""
    r = np.where(sl < 20, 0.77, np.where(sl < 40, 0.91, np.where(sl < 60, 0.98, 0.12)))
    g = np.where(sl < 20, 0.13, np.where(sl < 40, 0.44, np.where(sl < 60, 0.67, 0.56)))
    b = np.where(sl < 20, 0.12, np.where(sl < 40, 0.04, np.where(sl < 60, 0.00, 0.24)))
    return r, g, b


# ════════════════════════════════════════════════════
# WebSocket Handler
# ════════════════════════════════════════════════════

class CarData:
    def __init__(self):
        self.lock = threading.Lock()
        self.cars = {}
        self.connected = False

    # --- websocket-client callbacks ---
    def _on_message(self, ws, message):
        try:
            data = json.loads(message)
            if data.get("type") != "cars":
                return
            now = time.time()
            with self.lock:
                for c in data["data"]:
                    cid = c["car_id"]
                    lx, lz = to_local(c["x"], c["y"])
                    if cid in self.cars:
                        car = self.cars[cid]
                        # If the jump is too large (reroute/teleport), snap
                        dx = lx - car["cx"]
                        dz = lz - car["cz"]
                        if dx * dx + dz * dz > 200 * 200:
                            car["cx"] = lx
                            car["cz"] = lz
                        car["tx"] = lx
                        car["tz"] = lz
                        car["speed"] = c["speed"]
                        car["t"] = now
                    else:
                        self.cars[cid] = dict(cx=lx, cz=lz, tx=lx, tz=lz,
                                              speed=c["speed"], t=now)
                # purge stale
                stale = [k for k, v in self.cars.items() if now - v["t"] > 15]
                for k in stale:
                    del self.cars[k]
        except Exception as e:
            print(f"[WS] parse error: {e}")

    def _on_open(self, ws):
        self.connected = True
        print("[WS] connected")

    def _on_close(self, ws, code, msg):
        self.connected = False
        print("[WS] closed — reconnecting in 3 s")
        time.sleep(3)
        self._connect()

    def _on_error(self, ws, error):
        pass  # on_close will fire next

    def _connect(self):
        ws = websocket.WebSocketApp(
            WS_URL,
            on_message=self._on_message,
            on_open=self._on_open,
            on_close=self._on_close,
            on_error=self._on_error,
        )
        t = threading.Thread(target=ws.run_forever, daemon=True)
        t.start()

    def start(self):
        self._connect()

    def snapshot(self, dt):
        """Return list of (x, z, speed) with lerp applied."""
        factor = 1.0 - math.exp(-3.0 * dt)
        out = []
        with self.lock:
            for c in self.cars.values():
                c["cx"] += (c["tx"] - c["cx"]) * factor
                c["cz"] += (c["tz"] - c["cz"]) * factor
                out.append((c["cx"], c["cz"], c["speed"]))
        return out


# ════════════════════════════════════════════════════
# Building Generator
# ════════════════════════════════════════════════════

def _box(bx, bz, hw, hd, h, shade):
    """Return (verts_list, normals_list, colors_list) for one box."""
    color = (shade, shade * 0.97, shade * 0.94)
    verts = [
        # Front (+Z)
        (bx - hw, 0, bz + hd), (bx + hw, 0, bz + hd), (bx + hw, h, bz + hd),
        (bx - hw, 0, bz + hd), (bx + hw, h, bz + hd), (bx - hw, h, bz + hd),
        # Back (-Z)
        (bx + hw, 0, bz - hd), (bx - hw, 0, bz - hd), (bx - hw, h, bz - hd),
        (bx + hw, 0, bz - hd), (bx - hw, h, bz - hd), (bx + hw, h, bz - hd),
        # Left (-X)
        (bx - hw, 0, bz - hd), (bx - hw, 0, bz + hd), (bx - hw, h, bz + hd),
        (bx - hw, 0, bz - hd), (bx - hw, h, bz + hd), (bx - hw, h, bz - hd),
        # Right (+X)
        (bx + hw, 0, bz + hd), (bx + hw, 0, bz - hd), (bx + hw, h, bz - hd),
        (bx + hw, 0, bz + hd), (bx + hw, h, bz - hd), (bx + hw, h, bz + hd),
        # Top
        (bx - hw, h, bz + hd), (bx + hw, h, bz + hd), (bx + hw, h, bz - hd),
        (bx - hw, h, bz + hd), (bx + hw, h, bz - hd), (bx - hw, h, bz - hd),
    ]
    normals = (
        [(0, 0, 1)] * 6 +
        [(0, 0, -1)] * 6 +
        [(-1, 0, 0)] * 6 +
        [(1, 0, 0)] * 6 +
        [(0, 1, 0)] * 6
    )
    colors = [color] * 30
    return verts, normals, colors


def generate_buildings(nodes_local, conn_count):
    rng = np.random.RandomState(42)
    intersections = [i for i, c in enumerate(conn_count) if c >= 3]
    if len(intersections) > MAX_BUILDINGS:
        intersections = rng.choice(intersections, MAX_BUILDINGS, replace=False).tolist()

    all_v, all_n, all_c = [], [], []
    for idx in intersections:
        x, z = nodes_local[idx]
        nb = rng.randint(1, 4)
        for _ in range(nb):
            ox = rng.uniform(-50, 50)
            oz = rng.uniform(-50, 50)
            if abs(ox) < 15:
                ox = 15.0 * (1 if ox >= 0 else -1)
            if abs(oz) < 15:
                oz = 15.0 * (1 if oz >= 0 else -1)
            w = rng.uniform(12, 38)
            d = rng.uniform(12, 38)
            h = rng.uniform(8, 42)
            shade = rng.uniform(0.68, 0.84)
            v, n, c = _box(x + ox, z + oz, w / 2, d / 2, h, shade)
            all_v.extend(v)
            all_n.extend(n)
            all_c.extend(c)

    if not all_v:
        return np.array([], dtype='f4'), np.array([], dtype='f4'), np.array([], dtype='f4')
    return (
        np.array(all_v, dtype='f4').reshape(-1),
        np.array(all_n, dtype='f4').reshape(-1),
        np.array(all_c, dtype='f4').reshape(-1),
    )


# ════════════════════════════════════════════════════
# Road Geometry Builder (vectorised)
# ════════════════════════════════════════════════════

def build_road_quads(nodes_x, nodes_z, edges_flat, num_edges):
    """
    Build thick road quads (two triangles per edge) for BOTH the casing
    (darker outline) and the fill (traffic-coloured).
    Returns (casing_array, fill_array) each shaped (N*6, 6) = pos+color per vert.
    """
    fi = edges_flat[0::4].astype(int)
    ti = edges_flat[1::4].astype(int)
    sl = edges_flat[2::4]

    fx, fz = nodes_x[fi], nodes_z[fi]
    tx, tz = nodes_x[ti], nodes_z[ti]

    dx = tx - fx
    dz = tz - fz
    lengths = np.sqrt(dx * dx + dz * dz)
    lengths[lengths < 0.001] = 0.001
    nx = -dz / lengths
    nz = dx / lengths

    # Filter out very long edges (routing shortcuts / long highway segments
    # that render as ugly straight lines across the map)
    keep = lengths < 1500.0  # 1.5 km max
    fi = fi[keep]; ti = ti[keep]; sl = sl[keep]
    fx = fx[keep]; fz = fz[keep]; tx = tx[keep]; tz = tz[keep]
    dx = dx[keep]; dz = dz[keep]; lengths = lengths[keep]
    nx = nx[keep]; nz = nz[keep]
    num_edges = int(keep.sum())

    # Width by road class
    base_hw = np.where(sl >= 80, 5.0,
              np.where(sl >= 50, 3.5,
              np.where(sl >= 30, 2.5, 1.8)))

    def _make_quads(hw_arr, y_val, r, g, b):
        hw = hw_arr
        v0x = fx + nx * hw;  v0z = fz + nz * hw
        v1x = fx - nx * hw;  v1z = fz - nz * hw
        v2x = tx + nx * hw;  v2z = tz + nz * hw
        v3x = tx - nx * hw;  v3z = tz - nz * hw
        y = np.full_like(v0x, y_val)
        # 6 verts per edge (2 tris)
        pos = np.zeros((num_edges * 6, 3), dtype='f4')
        col = np.zeros((num_edges * 6, 3), dtype='f4')
        for k, (vx, vz) in enumerate([
            (v0x, v0z), (v1x, v1z), (v2x, v2z),
            (v1x, v1z), (v3x, v3z), (v2x, v2z),
        ]):
            pos[k::6, 0] = vx
            pos[k::6, 1] = y
            pos[k::6, 2] = vz
            col[k::6, 0] = r
            col[k::6, 1] = g
            col[k::6, 2] = b
        return np.hstack([pos, col]).astype('f4')

    # Casing (wider, dark grey)
    cas_r = np.full(num_edges, 0.62, dtype='f4')
    cas_g = np.full(num_edges, 0.61, dtype='f4')
    cas_b = np.full(num_edges, 0.59, dtype='f4')
    casing = _make_quads(base_hw * 1.5, 0.2, cas_r, cas_g, cas_b)

    # Fill (traffic colour)
    fr, fg, fb = speed_color_vec(sl)
    fill = _make_quads(base_hw, 0.5, fr.astype('f4'), fg.astype('f4'), fb.astype('f4'))

    return casing, fill


# ════════════════════════════════════════════════════
# Main Application
# ════════════════════════════════════════════════════

class App:
    def __init__(self):
        pygame.init()
        pygame.display.set_caption("Waze Traffic Simulation")
        pygame.display.gl_set_attribute(pygame.GL_CONTEXT_MAJOR_VERSION, 3)
        pygame.display.gl_set_attribute(pygame.GL_CONTEXT_MINOR_VERSION, 3)
        pygame.display.gl_set_attribute(pygame.GL_CONTEXT_PROFILE_MASK,
                                        pygame.GL_CONTEXT_PROFILE_CORE)
        pygame.display.gl_set_attribute(pygame.GL_MULTISAMPLEBUFFERS, 1)
        pygame.display.gl_set_attribute(pygame.GL_MULTISAMPLESAMPLES, 4)

        self.screen = pygame.display.set_mode(
            (WIN_W, WIN_H),
            pygame.OPENGL | pygame.DOUBLEBUF | pygame.RESIZABLE,
        )
        self.w, self.h = WIN_W, WIN_H
        self.ctx = moderngl.create_context()
        self.ctx.enable(moderngl.DEPTH_TEST)
        self.ctx.enable(moderngl.BLEND)
        self.ctx.enable(moderngl.PROGRAM_POINT_SIZE)
        self.ctx.blend_func = moderngl.SRC_ALPHA, moderngl.ONE_MINUS_SRC_ALPHA

        # --- shader programs ---
        self.prog_pc = self.ctx.program(vertex_shader=VERT_PC, fragment_shader=FRAG_PC)
        self.prog_bld = self.ctx.program(vertex_shader=VERT_BLD, fragment_shader=FRAG_BLD)
        self.prog_car = self.ctx.program(vertex_shader=VERT_CAR, fragment_shader=FRAG_CAR)
        self.prog_hud = self.ctx.program(vertex_shader=VERT_HUD, fragment_shader=FRAG_HUD)

        # --- HUD quad ---
        hud_quad = np.array([
            0, 0, 0, 0,
            1, 0, 1, 0,
            1, 1, 1, 1,
            0, 0, 0, 0,
            1, 1, 1, 1,
            0, 1, 0, 1,
        ], dtype='f4')
        self.hud_vao = self.ctx.vertex_array(
            self.prog_hud,
            [(self.ctx.buffer(hud_quad.tobytes()), '2f 2f', 'in_position', 'in_texcoord')],
        )
        self.hud_tex = None

        # --- ground plane ---
        sz = 25000.0
        gnd = np.array([
            -sz, -0.1, -sz,  0.90, 0.89, 0.86,
             sz, -0.1, -sz,  0.90, 0.89, 0.86,
             sz, -0.1,  sz,  0.90, 0.89, 0.86,
            -sz, -0.1, -sz,  0.90, 0.89, 0.86,
             sz, -0.1,  sz,  0.90, 0.89, 0.86,
            -sz, -0.1,  sz,  0.90, 0.89, 0.86,
        ], dtype='f4')
        self.ground_vao = self.ctx.vertex_array(
            self.prog_pc,
            [(self.ctx.buffer(gnd.tobytes()), '3f 3f', 'in_position', 'in_color')],
        )

        # --- camera ---
        self.cam_x = 0.0
        self.cam_z = 0.0
        self.cam_h = CAM_START_HEIGHT
        self.tilt = math.radians(CAM_TILT_DEG)
        self.dragging = False
        self.last_mouse = (0, 0)

        # --- data ---
        self.car_data = CarData()
        self.road_count = 0
        self.car_count = 0
        self.avg_speed = 0.0

        self.casing_vao = None
        self.fill_vao = None
        self.bld_vao = None
        self.bld_vert_count = 0
        self.car_vao = None
        self.car_vbo = None

        self.font = pygame.font.SysFont("Arial", 14, bold=True)
        self.font_title = pygame.font.SysFont("Arial", 16, bold=True)
        self.clock = pygame.time.Clock()
        self.hud_timer = 0.0

    # ─── load graph from API ──────────────────────
    def load_graph(self):
        print("Fetching graph from server...")
        try:
            resp = requests.get(GRAPH_URL, timeout=30)
            data = resp.json()
        except Exception as e:
            print(f"Cannot connect to {SERVER_URL}: {e}")
            print("Start the Go server first, then run this script.")
            return False

        nf = np.array(data["nodes"], dtype='f4')
        ef = np.array(data["edges"], dtype='f4')
        num_nodes = len(nf) // 2
        num_edges = len(ef) // 4
        self.road_count = num_edges
        print(f"  {num_nodes:,} nodes, {num_edges:,} edges")

        # Convert nodes to local coords
        lons = nf[0::2]
        lats = nf[1::2]
        nodes_x = ((lons - CENTER_LON) * DEG_TO_M_LON).astype('f4')
        nodes_z = (-(lats - CENTER_LAT) * DEG_TO_M_LAT).astype('f4')

        # Build road quads (vectorised)
        print("  Building road geometry...")
        casing_data, fill_data = build_road_quads(nodes_x, nodes_z, ef, num_edges)

        self.casing_vao = self.ctx.vertex_array(
            self.prog_pc,
            [(self.ctx.buffer(casing_data.tobytes()), '3f 3f', 'in_position', 'in_color')],
        )
        self.casing_count = num_edges * 6

        self.fill_vao = self.ctx.vertex_array(
            self.prog_pc,
            [(self.ctx.buffer(fill_data.tobytes()), '3f 3f', 'in_position', 'in_color')],
        )
        self.fill_count = num_edges * 6

        # Connection count per node for building placement
        conn = np.zeros(num_nodes, dtype=int)
        fi = ef[0::4].astype(int)
        ti = ef[1::4].astype(int)
        np.add.at(conn, fi, 1)
        np.add.at(conn, ti, 1)

        nodes_local = list(zip(nodes_x.tolist(), nodes_z.tolist()))

        print("  Generating buildings...")
        bv, bn, bc = generate_buildings(nodes_local, conn.tolist())
        if len(bv) > 0:
            self.bld_vao = self.ctx.vertex_array(
                self.prog_bld,
                [
                    (self.ctx.buffer(bv.tobytes()), '3f', 'in_position'),
                    (self.ctx.buffer(bn.tobytes()), '3f', 'in_normal'),
                    (self.ctx.buffer(bc.tobytes()), '3f', 'in_color'),
                ],
            )
            self.bld_vert_count = len(bv) // 3
            print(f"  {self.bld_vert_count // 30:,} buildings")

        print("  Done.")
        return True

    # ─── MVP matrix ───────────────────────────────
    def mvp(self):
        aspect = self.w / self.h
        proj = perspective(FOV, aspect, NEAR_CLIP, FAR_CLIP)
        # Camera: above and behind the look-at point
        eye = np.array([
            self.cam_x,
            self.cam_h,
            self.cam_z + self.cam_h / math.tan(self.tilt),
        ], dtype='f4')
        target = np.array([self.cam_x, 0.0, self.cam_z], dtype='f4')
        view = look_at(eye, target, [0, 1, 0])
        # Transpose for GLSL column-major convention
        return np.ascontiguousarray((proj @ view).T, dtype='f4')

    # ─── events ───────────────────────────────────
    def events(self):
        for ev in pygame.event.get():
            if ev.type == QUIT:
                return False
            elif ev.type == KEYDOWN:
                if ev.key == K_ESCAPE:
                    return False
                if ev.key == K_r:
                    self.cam_x = self.cam_z = 0.0
                    self.cam_h = CAM_START_HEIGHT
            elif ev.type == MOUSEBUTTONDOWN:
                if ev.button == 1:
                    self.dragging = True
                    self.last_mouse = ev.pos
                elif ev.button == 4:
                    self.cam_h = max(80, self.cam_h * 0.88)
                elif ev.button == 5:
                    self.cam_h = min(15000, self.cam_h * 1.12)
            elif ev.type == MOUSEBUTTONUP:
                if ev.button == 1:
                    self.dragging = False
            elif ev.type == MOUSEMOTION:
                if self.dragging:
                    dx = ev.pos[0] - self.last_mouse[0]
                    dy = ev.pos[1] - self.last_mouse[1]
                    scale = self.cam_h * 0.0018
                    self.cam_x -= dx * scale
                    self.cam_z -= dy * scale
                    self.last_mouse = ev.pos
            elif ev.type == VIDEORESIZE:
                self.w, self.h = ev.w, ev.h
                self.ctx.viewport = (0, 0, ev.w, ev.h)
        return True

    # ─── update cars ──────────────────────────────
    def update_cars(self, dt):
        positions = self.car_data.snapshot(dt)
        self.car_count = len(positions)
        if not positions:
            self.avg_speed = 0
            return

        arr = np.zeros((len(positions), 6), dtype='f4')
        total_spd = 0.0
        for i, (x, z, spd) in enumerate(positions):
            r, g, b = speed_color(spd)
            arr[i] = (x, 3.0, z, r, g, b)
            total_spd += spd
        self.avg_speed = total_spd / len(positions)

        if self.car_vbo is not None:
            self.car_vbo.release()
        self.car_vbo = self.ctx.buffer(arr.tobytes())
        if self.car_vao is not None:
            self.car_vao.release()
        self.car_vao = self.ctx.vertex_array(
            self.prog_car,
            [(self.car_vbo, '3f 3f', 'in_position', 'in_color')],
        )

    # ─── render HUD overlay ──────────────────────
    def render_hud(self):
        hud_w, hud_h = 210, 130
        surf = pygame.Surface((hud_w, hud_h), pygame.SRCALPHA)
        # Background with rounded corners
        pygame.draw.rect(surf, (255, 255, 255, 215), (0, 0, hud_w, hud_h), border_radius=10)
        pygame.draw.rect(surf, (200, 200, 200, 180), (0, 0, hud_w, hud_h), 1, border_radius=10)

        # Title
        t = self.font_title.render("WAZE TRAFFIC", True, (26, 115, 232))
        surf.blit(t, (14, 10))

        # Divider line
        pygame.draw.line(surf, (220, 220, 220), (14, 32), (hud_w - 14, 32))

        # Stats
        y = 40
        stats = [
            ("Vehicles", str(self.car_count)),
            ("Avg Speed", f"{self.avg_speed:.0f} km/h" if self.avg_speed > 0 else "\u2014"),
            ("Roads", f"{self.road_count:,}"),
        ]
        for label, value in stats:
            lbl = self.font.render(label, True, (102, 102, 102))
            val = self.font.render(value, True, (34, 34, 34))
            surf.blit(lbl, (14, y))
            surf.blit(val, (hud_w - val.get_width() - 14, y))
            y += 24

        # Connection status
        status_color = (30, 142, 62) if self.car_data.connected else (197, 34, 31)
        status_text = "LIVE" if self.car_data.connected else "OFFLINE"
        st = self.font.render(status_text, True, status_color)
        surf.blit(st, (14, y + 4))

        # Upload to GL texture
        raw = pygame.image.tostring(surf, 'RGBA', True)
        if self.hud_tex is not None:
            self.hud_tex.release()
        self.hud_tex = self.ctx.texture((hud_w, hud_h), 4, raw)
        self.hud_tex.filter = (moderngl.LINEAR, moderngl.LINEAR)

        # Compute screen-space rect in NDC
        mx = 16.0 / self.w * 2.0
        my = 16.0 / self.h * 2.0
        nw = hud_w / self.w * 2.0
        nh = hud_h / self.h * 2.0
        # Bottom-left of the HUD panel in NDC
        rx = -1.0 + mx
        ry = 1.0 - my - nh
        self.prog_hud['rect'].value = (rx, ry, nw, nh)

        self.ctx.disable(moderngl.DEPTH_TEST)
        self.hud_tex.use()
        self.hud_vao.render(moderngl.TRIANGLES)
        self.ctx.enable(moderngl.DEPTH_TEST)

    # ─── render frame ─────────────────────────────
    def render(self, dt):
        self.ctx.clear(0.94, 0.93, 0.91, 1.0)
        self.ctx.viewport = (0, 0, self.w, self.h)

        m = self.mvp()
        m_bytes = m.tobytes()

        # Ground — don't write depth so it never occludes roads/buildings
        self.ctx.depth_mask = False
        self.prog_pc['mvp'].write(m_bytes)
        self.ground_vao.render(moderngl.TRIANGLES)
        self.ctx.depth_mask = True

        # Road casing
        if self.casing_vao:
            self.prog_pc['mvp'].write(m_bytes)
            self.casing_vao.render(moderngl.TRIANGLES)

        # Road fill
        if self.fill_vao:
            self.prog_pc['mvp'].write(m_bytes)
            self.fill_vao.render(moderngl.TRIANGLES)

        # Buildings
        if self.bld_vao and self.bld_vert_count > 0:
            self.prog_bld['mvp'].write(m_bytes)
            self.prog_bld['light_dir'].value = (0.4, 0.75, 0.35)
            self.bld_vao.render(moderngl.TRIANGLES)

        # Cars
        if self.car_vao and self.car_count > 0:
            self.prog_car['mvp'].write(m_bytes)
            pt = max(5.0, min(24.0, 18.0 * (1200.0 / self.cam_h)))
            self.prog_car['point_size'].value = pt
            self.car_vao.render(moderngl.POINTS)

        # HUD (update every 400 ms)
        self.hud_timer += dt
        if self.hud_timer >= 0.4:
            self.hud_timer = 0.0
        self.render_hud()

    # ─── main loop ────────────────────────────────
    def run(self):
        if not self.load_graph():
            return
        self.car_data.start()
        print("Running. ESC to quit, R to reset camera, drag to pan, scroll to zoom.")

        running = True
        while running:
            dt = self.clock.tick(60) / 1000.0
            running = self.events()
            if running is None:
                running = True
            self.update_cars(dt)
            self.render(dt)
            pygame.display.flip()

        pygame.quit()


# ════════════════════════════════════════════════════
if __name__ == "__main__":
    App().run()
