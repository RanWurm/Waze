// web/drive.js — Immersive 3D First-Person Driving View
// Requires Three.js 0.160.0 loaded globally

const DriveView = (function () {
  'use strict';

  // ===== Constants =====
  const SCALE = 50000;
  const ROAD_HALF_WIDTH = 2;    // ~3.8m real
  const SIDEWALK_WIDTH = 0.8;   // ~1.5m real
  const CAMERA_HEIGHT = 1.2;    // ~2.3m real (driver eye level)
  const CAMERA_SMOOTH = 12;
  const LOOK_SMOOTH = 4;
  const LOOK_AHEAD_KM = 0.0008; // ~80m
  const FOG_DENSITY = 0.004;
  const MAX_BUILDINGS_PER_SIDE = 2;

  // ===== Module State =====
  let scene, camera, renderer, clock;
  let routeGroup, envGroup;
  let roadMaterial; // ShaderMaterial
  let origin = { lng: 0, lat: 0 };
  let worldPoints = []; // pre-computed world coords per edge
  let isAnimating = false;
  let animFrameId = null;
  let routeBuilt = false;

  // Camera smoothing
  const smoothPos = new THREE.Vector3();
  const smoothLookAt = new THREE.Vector3();
  let cameraInitialized = false;

  // Headlights
  let headlightL, headlightR;

  // ===== Coordinate Conversion =====
  function geoToWorld(lng, lat) {
    return new THREE.Vector3(
      (lng - origin.lng) * SCALE,
      0,
      -(lat - origin.lat) * SCALE
    );
  }

  // ===== Procedural Textures =====
  function createAsphaltTexture() {
    const c = document.createElement('canvas');
    c.width = 128; c.height = 128;
    const ctx = c.getContext('2d');
    ctx.fillStyle = '#333338';
    ctx.fillRect(0, 0, 128, 128);
    // Subtle noise
    for (let i = 0; i < 800; i++) {
      const x = Math.random() * 128;
      const y = Math.random() * 128;
      const v = 40 + Math.random() * 20;
      ctx.fillStyle = `rgb(${v},${v},${v})`;
      ctx.fillRect(x, y, 1, 1);
    }
    const tex = new THREE.CanvasTexture(c);
    tex.wrapS = tex.wrapT = THREE.RepeatWrapping;
    tex.repeat.set(4, 4);
    return tex;
  }

  function createWindowTexture() {
    const c = document.createElement('canvas');
    c.width = 64; c.height = 128;
    const ctx = c.getContext('2d');
    // Building wall base
    ctx.fillStyle = '#555560';
    ctx.fillRect(0, 0, 64, 128);
    // Window grid
    const cols = 4, rows = 8;
    const ww = 10, wh = 10;
    const gx = (64 - cols * ww) / (cols + 1);
    const gy = (128 - rows * wh) / (rows + 1);
    for (let r = 0; r < rows; r++) {
      for (let cc = 0; cc < cols; cc++) {
        const lit = Math.random() > 0.4;
        ctx.fillStyle = lit ? '#ffe4a0' : '#2a2a30';
        const x = gx + cc * (ww + gx);
        const y = gy + r * (wh + gy);
        ctx.fillRect(x, y, ww, wh);
      }
    }
    const tex = new THREE.CanvasTexture(c);
    tex.wrapS = tex.wrapT = THREE.RepeatWrapping;
    return tex;
  }

  function createSkyBackground() {
    const c = document.createElement('canvas');
    c.width = 2; c.height = 512;
    const ctx = c.getContext('2d');
    const grad = ctx.createLinearGradient(0, 0, 0, 512);
    grad.addColorStop(0, '#050520');
    grad.addColorStop(0.3, '#0a0a3a');
    grad.addColorStop(0.6, '#151540');
    grad.addColorStop(0.85, '#252545');
    grad.addColorStop(1.0, '#353555');
    ctx.fillStyle = grad;
    ctx.fillRect(0, 0, 2, 512);
    const tex = new THREE.CanvasTexture(c);
    return tex;
  }

  // ===== Geometry Builders =====

  function precomputeWorldPoints(routeEdges) {
    worldPoints = [];
    for (let i = 0; i < routeEdges.length; i++) {
      const e = routeEdges[i];
      const start = geoToWorld(e.from_x, e.from_y);
      const end = geoToWorld(e.to_x, e.to_y);
      const dir = new THREE.Vector3().subVectors(end, start);
      const len = dir.length();
      dir.normalize();
      const perp = new THREE.Vector3(-dir.z, 0, dir.x);
      worldPoints.push({ start, end, dir, perp, len, edgeIndex: i });
    }
  }

  function buildRoadGeometry() {
    // Vertex shader: passes edge index to fragment
    const vertShader = `
      attribute float aEdgeIndex;
      varying float vEdgeIndex;
      varying vec2 vUv;
      void main() {
        vEdgeIndex = aEdgeIndex;
        vUv = uv;
        gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
      }
    `;

    // Fragment shader: green for traveled, purple for upcoming
    const fragShader = `
      uniform float uTraveledProgress;
      varying float vEdgeIndex;
      varying vec2 vUv;
      void main() {
        vec3 green = vec3(0.251, 0.753, 0.345);
        vec3 purple = vec3(0.659, 0.333, 0.969);
        vec3 color = vEdgeIndex < uTraveledProgress ? green : purple;
        // Darken edges of road for depth
        float edgeFade = 1.0 - 0.3 * pow(abs(vUv.x - 0.5) * 2.0, 2.0);
        gl_FragColor = vec4(color * edgeFade, 1.0);
      }
    `;

    const numEdges = worldPoints.length;
    // 4 vertices per edge, 6 indices per edge (2 triangles)
    const positions = new Float32Array(numEdges * 4 * 3);
    const uvs = new Float32Array(numEdges * 4 * 2);
    const edgeIndices = new Float32Array(numEdges * 4);
    const indices = new Uint32Array(numEdges * 6);

    for (let i = 0; i < numEdges; i++) {
      const wp = worldPoints[i];
      const hw = ROAD_HALF_WIDTH;

      // Compute miter at start
      let startPerp = wp.perp.clone();
      if (i > 0) {
        const prevPerp = worldPoints[i - 1].perp;
        startPerp.add(prevPerp).normalize();
        // Clamp miter length
        const dot = startPerp.dot(wp.perp);
        if (dot > 0.1) startPerp.multiplyScalar(1 / dot);
      }

      // Compute miter at end
      let endPerp = wp.perp.clone();
      if (i < numEdges - 1) {
        const nextPerp = worldPoints[i + 1].perp;
        endPerp.add(nextPerp).normalize();
        const dot = endPerp.dot(wp.perp);
        if (dot > 0.1) endPerp.multiplyScalar(1 / dot);
      }

      const vi = i * 4;
      const pi = vi * 3;
      const ui = vi * 2;

      // start-left
      positions[pi + 0] = wp.start.x - startPerp.x * hw;
      positions[pi + 1] = 0.02;
      positions[pi + 2] = wp.start.z - startPerp.z * hw;
      // start-right
      positions[pi + 3] = wp.start.x + startPerp.x * hw;
      positions[pi + 4] = 0.02;
      positions[pi + 5] = wp.start.z + startPerp.z * hw;
      // end-left
      positions[pi + 6] = wp.end.x - endPerp.x * hw;
      positions[pi + 7] = 0.02;
      positions[pi + 8] = wp.end.z - endPerp.z * hw;
      // end-right
      positions[pi + 9] = wp.end.x + endPerp.x * hw;
      positions[pi + 10] = 0.02;
      positions[pi + 11] = wp.end.z + endPerp.z * hw;

      // UVs: x=0 left, x=1 right; y along length
      uvs[ui + 0] = 0; uvs[ui + 1] = 0;
      uvs[ui + 2] = 1; uvs[ui + 3] = 0;
      uvs[ui + 4] = 0; uvs[ui + 5] = 1;
      uvs[ui + 6] = 1; uvs[ui + 7] = 1;

      // Edge index attribute
      edgeIndices[vi + 0] = i;
      edgeIndices[vi + 1] = i;
      edgeIndices[vi + 2] = i;
      edgeIndices[vi + 3] = i;

      // Two triangles
      const ii = i * 6;
      indices[ii + 0] = vi + 0;
      indices[ii + 1] = vi + 1;
      indices[ii + 2] = vi + 2;
      indices[ii + 3] = vi + 1;
      indices[ii + 4] = vi + 3;
      indices[ii + 5] = vi + 2;
    }

    const geom = new THREE.BufferGeometry();
    geom.setAttribute('position', new THREE.BufferAttribute(positions, 3));
    geom.setAttribute('uv', new THREE.BufferAttribute(uvs, 2));
    geom.setAttribute('aEdgeIndex', new THREE.BufferAttribute(edgeIndices, 1));
    geom.setIndex(new THREE.BufferAttribute(indices, 1));

    roadMaterial = new THREE.ShaderMaterial({
      vertexShader: vertShader,
      fragmentShader: fragShader,
      uniforms: {
        uTraveledProgress: { value: 0.0 }
      },
      side: THREE.DoubleSide
    });

    const mesh = new THREE.Mesh(geom, roadMaterial);
    routeGroup.add(mesh);
  }

  function buildEdgeLines() {
    const numEdges = worldPoints.length;
    const positions = [];

    for (let i = 0; i < numEdges; i++) {
      const wp = worldPoints[i];
      const hw = ROAD_HALF_WIDTH + 0.2;

      // Left edge line
      positions.push(
        wp.start.x - wp.perp.x * hw, 0.04, wp.start.z - wp.perp.z * hw,
        wp.end.x - wp.perp.x * hw, 0.04, wp.end.z - wp.perp.z * hw
      );
      // Right edge line
      positions.push(
        wp.start.x + wp.perp.x * hw, 0.04, wp.start.z + wp.perp.z * hw,
        wp.end.x + wp.perp.x * hw, 0.04, wp.end.z + wp.perp.z * hw
      );
    }

    const geom = new THREE.BufferGeometry();
    geom.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
    const mat = new THREE.LineBasicMaterial({ color: 0xffffff, linewidth: 1 });
    const lines = new THREE.LineSegments(geom, mat);
    routeGroup.add(lines);
  }

  function buildCenterDashes() {
    const positions = [];
    const indices = [];
    let vi = 0;
    const dashLen = 1.5;   // length of each dash in world units
    const gapLen = 1.0;    // gap between dashes
    const dashWidth = 0.15; // width of dash line

    for (let i = 0; i < worldPoints.length; i++) {
      const wp = worldPoints[i];
      const cycle = dashLen + gapLen;
      const numDashes = Math.floor(wp.len / cycle);

      for (let d = 0; d < numDashes; d++) {
        const tStart = (d * cycle) / wp.len;
        const tEnd = (d * cycle + dashLen) / wp.len;
        if (tEnd > 1) break;

        const s = new THREE.Vector3().lerpVectors(wp.start, wp.end, tStart);
        const e = new THREE.Vector3().lerpVectors(wp.start, wp.end, tEnd);

        // Quad: 4 vertices offset by dashWidth along perpendicular
        positions.push(
          s.x - wp.perp.x * dashWidth, 0.05, s.z - wp.perp.z * dashWidth,
          s.x + wp.perp.x * dashWidth, 0.05, s.z + wp.perp.z * dashWidth,
          e.x - wp.perp.x * dashWidth, 0.05, e.z - wp.perp.z * dashWidth,
          e.x + wp.perp.x * dashWidth, 0.05, e.z + wp.perp.z * dashWidth
        );
        indices.push(vi, vi + 1, vi + 2, vi + 1, vi + 3, vi + 2);
        vi += 4;
      }
    }

    if (positions.length === 0) return;

    const geom = new THREE.BufferGeometry();
    geom.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
    geom.setIndex(indices);

    const mat = new THREE.MeshBasicMaterial({ color: 0xffcc00, side: THREE.DoubleSide });
    routeGroup.add(new THREE.Mesh(geom, mat));
  }

  function buildSidewalks() {
    const numEdges = worldPoints.length;
    const positions = [];
    const indices = [];
    let vi = 0;

    for (let i = 0; i < numEdges; i++) {
      const wp = worldPoints[i];
      const innerOffset = ROAD_HALF_WIDTH + 0.3;
      const outerOffset = ROAD_HALF_WIDTH + 0.3 + SIDEWALK_WIDTH;

      // Left sidewalk
      for (const side of [-1, 1]) {
        const iOff = side * innerOffset;
        const oOff = side * outerOffset;

        positions.push(
          wp.start.x + wp.perp.x * iOff, 0.06, wp.start.z + wp.perp.z * iOff,
          wp.start.x + wp.perp.x * oOff, 0.06, wp.start.z + wp.perp.z * oOff,
          wp.end.x + wp.perp.x * iOff, 0.06, wp.end.z + wp.perp.z * iOff,
          wp.end.x + wp.perp.x * oOff, 0.06, wp.end.z + wp.perp.z * oOff
        );
        indices.push(vi, vi + 1, vi + 2, vi + 1, vi + 3, vi + 2);
        vi += 4;
      }
    }

    const geom = new THREE.BufferGeometry();
    geom.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
    geom.setIndex(indices);
    geom.computeVertexNormals();

    const mat = new THREE.MeshStandardMaterial({
      color: 0x888888,
      roughness: 0.9
    });

    routeGroup.add(new THREE.Mesh(geom, mat));
  }

  function buildBuildings() {
    const buildingGeom = new THREE.BoxGeometry(1, 1, 1);
    const windowTex = createWindowTexture();
    const buildingMat = new THREE.MeshStandardMaterial({
      map: windowTex,
      roughness: 0.8
    });

    // Count total buildings
    let totalBuildings = 0;
    for (let i = 0; i < worldPoints.length; i++) {
      totalBuildings += MAX_BUILDINGS_PER_SIDE * 2;
    }

    const instancedMesh = new THREE.InstancedMesh(buildingGeom, buildingMat, totalBuildings);
    const dummy = new THREE.Object3D();
    const color = new THREE.Color();
    let idx = 0;

    // Seeded random for consistent buildings
    let seed = 12345;
    function seededRandom() {
      seed = (seed * 16807 + 0) % 2147483647;
      return (seed - 1) / 2147483646;
    }

    for (let i = 0; i < worldPoints.length; i++) {
      const wp = worldPoints[i];
      // Skip short edges — not enough room for buildings
      if (wp.len < 8) continue;
      const buildingSetback = ROAD_HALF_WIDTH + SIDEWALK_WIDTH + 3;
      const numBuildings = Math.min(MAX_BUILDINGS_PER_SIDE, Math.floor(wp.len / 10));
      if (numBuildings < 1) continue;

      for (const side of [-1, 1]) {
        for (let b = 0; b < numBuildings; b++) {
          const t = (b + 0.5) / numBuildings;
          const pos = new THREE.Vector3().lerpVectors(wp.start, wp.end, t);

          const width = 3 + seededRandom() * 5;
          const depth = 3 + seededRandom() * 4;
          const height = 8 + seededRandom() * 25;
          const offset = buildingSetback + seededRandom() * 4;

          pos.x += wp.perp.x * side * offset;
          pos.z += wp.perp.z * side * offset;
          pos.y = height / 2;

          dummy.position.copy(pos);
          dummy.scale.set(width, height, depth);
          dummy.rotation.y = Math.atan2(wp.dir.x, wp.dir.z) + (seededRandom() - 0.5) * 0.3;
          dummy.updateMatrix();

          instancedMesh.setMatrixAt(idx, dummy.matrix);

          // Muted building colors
          const hue = seededRandom() * 0.1 + 0.05;
          const sat = 0.05 + seededRandom() * 0.1;
          const light = 0.3 + seededRandom() * 0.15;
          color.setHSL(hue, sat, light);
          instancedMesh.setColorAt(idx, color);

          idx++;
        }
      }
    }

    instancedMesh.count = idx;
    instancedMesh.instanceMatrix.needsUpdate = true;
    if (instancedMesh.instanceColor) instancedMesh.instanceColor.needsUpdate = true;

    envGroup.add(instancedMesh);
  }

  function buildGround() {
    const geom = new THREE.PlaneGeometry(4000, 4000);
    const mat = new THREE.MeshStandardMaterial({
      color: 0x1a2a1a,
      roughness: 0.95
    });
    const ground = new THREE.Mesh(geom, mat);
    ground.rotation.x = -Math.PI / 2;
    ground.position.y = -0.05;
    envGroup.add(ground);
  }

  // ===== Camera =====

  function computeLookAhead(state) {
    if (!state.routeEdges || state.carEdgeIndex >= worldPoints.length) {
      return smoothLookAt.clone();
    }

    const lookAheadWorld = LOOK_AHEAD_KM * SCALE;
    let distRemaining = lookAheadWorld;

    // Start from current position on current edge
    const wp = worldPoints[state.carEdgeIndex];
    const edge = state.routeEdges[state.carEdgeIndex];
    const progress = edge.length > 0 ? state.carEdgeProgress / edge.length : 0;
    const remainingOnEdge = (1 - progress) * wp.len;

    if (remainingOnEdge >= distRemaining) {
      const t = progress + (distRemaining / wp.len);
      return new THREE.Vector3().lerpVectors(wp.start, wp.end, Math.min(t, 1))
        .setY(CAMERA_HEIGHT);
    }

    // Walk through subsequent edges
    distRemaining -= remainingOnEdge;
    for (let i = state.carEdgeIndex + 1; i < worldPoints.length; i++) {
      const nextWp = worldPoints[i];
      if (nextWp.len >= distRemaining) {
        const t = distRemaining / nextWp.len;
        return new THREE.Vector3().lerpVectors(nextWp.start, nextWp.end, t)
          .setY(CAMERA_HEIGHT);
      }
      distRemaining -= nextWp.len;
    }

    // Past end of route
    const lastWp = worldPoints[worldPoints.length - 1];
    return lastWp.end.clone().setY(CAMERA_HEIGHT);
  }

  function updateCamera(state, dt) {
    // Clamp dt to prevent jumps from frame drops
    dt = Math.min(dt, 0.05);

    const targetPos = geoToWorld(state.carPosition.lng, state.carPosition.lat);
    targetPos.y = CAMERA_HEIGHT;

    const lookTarget = computeLookAhead(state);

    if (!cameraInitialized) {
      smoothPos.copy(targetPos);
      smoothLookAt.copy(lookTarget);
      cameraInitialized = true;
    }

    // Exponential smoothing (frame-rate independent)
    const posFactor = 1 - Math.exp(-CAMERA_SMOOTH * dt);
    const lookFactor = 1 - Math.exp(-LOOK_SMOOTH * dt);

    smoothPos.lerp(targetPos, posFactor);
    smoothLookAt.lerp(lookTarget, lookFactor);

    camera.position.copy(smoothPos);
    camera.lookAt(smoothLookAt);
  }

  // ===== Public API =====

  function init(containerId) {
    const container = document.getElementById(containerId);

    // Scene
    scene = new THREE.Scene();
    scene.fog = new THREE.FogExp2(0x252545, FOG_DENSITY);
    scene.background = createSkyBackground();

    // Camera
    camera = new THREE.PerspectiveCamera(
      70,
      container.clientWidth / container.clientHeight,
      0.1, 800
    );

    // Renderer
    renderer = new THREE.WebGLRenderer({ antialias: true });
    renderer.setSize(container.clientWidth, container.clientHeight);
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.shadowMap.enabled = false;
    container.appendChild(renderer.domElement);

    // Lighting
    const ambient = new THREE.AmbientLight(0x404060, 0.4);
    scene.add(ambient);

    const hemi = new THREE.HemisphereLight(0x101040, 0x0a1a0a, 0.3);
    scene.add(hemi);

    const moon = new THREE.DirectionalLight(0x8888cc, 0.3);
    moon.position.set(50, 100, 30);
    scene.add(moon);

    // Headlights attached to camera
    headlightL = new THREE.SpotLight(0xffffcc, 3.0, 120, 0.5, 0.4);
    headlightL.position.set(-1.5, -0.5, 0);
    headlightL.target.position.set(-2, -2, -50);
    camera.add(headlightL);
    camera.add(headlightL.target);

    headlightR = new THREE.SpotLight(0xffffcc, 3.0, 120, 0.5, 0.4);
    headlightR.position.set(1.5, -0.5, 0);
    headlightR.target.position.set(2, -2, -50);
    camera.add(headlightR);
    camera.add(headlightR.target);

    scene.add(camera);

    // Groups
    routeGroup = new THREE.Group();
    envGroup = new THREE.Group();
    scene.add(routeGroup);
    scene.add(envGroup);

    // Clock
    clock = new THREE.Clock();

    // Stars
    const starGeom = new THREE.BufferGeometry();
    const starCount = 1500;
    const starPos = new Float32Array(starCount * 3);
    for (let i = 0; i < starCount * 3; i += 3) {
      starPos[i] = (Math.random() - 0.5) * 1600;
      starPos[i + 1] = 80 + Math.random() * 200;
      starPos[i + 2] = (Math.random() - 0.5) * 1600;
    }
    starGeom.setAttribute('position', new THREE.BufferAttribute(starPos, 3));
    scene.add(new THREE.Points(starGeom, new THREE.PointsMaterial({ color: 0xffffff, size: 0.8 })));

    window.addEventListener('resize', resize);
  }

  function buildRoute(routeEdges) {
    if (!routeEdges || routeEdges.length === 0) return;

    // Dispose old geometry
    disposeGroup(routeGroup);
    disposeGroup(envGroup);

    // Set origin
    origin.lng = routeEdges[0].from_x;
    origin.lat = routeEdges[0].from_y;

    // Pre-compute world coordinates
    precomputeWorldPoints(routeEdges);

    // Build all geometry
    buildRoadGeometry();
    buildEdgeLines();
    buildCenterDashes();
    buildSidewalks();
    buildGround();
    buildBuildings();

    cameraInitialized = false;
    routeBuilt = true;
  }

  function update(state) {
    isDriving = state.isDriving;
    if (!routeBuilt || !state.isDriving) return;

    const dt = clock.getDelta();

    // Update road shader: traveled progress
    if (roadMaterial) {
      // Only fully completed edges are green; current + ahead are purple
      roadMaterial.uniforms.uTraveledProgress.value = state.carEdgeIndex;
    }

    // Update camera
    updateCamera(state, dt);

    // Render
    renderer.render(scene, camera);
  }

  function resize() {
    if (!renderer) return;
    const container = renderer.domElement.parentElement;
    if (!container || container.clientWidth === 0 || container.clientHeight === 0) return;
    camera.aspect = container.clientWidth / container.clientHeight;
    camera.updateProjectionMatrix();
    renderer.setSize(container.clientWidth, container.clientHeight);
  }

  function startAnimation() {
    if (isAnimating) return;
    isAnimating = true;
    // Resize now that container is visible
    resize();
    clock.getDelta(); // reset delta
    animate();
  }

  function stopAnimation() {
    isAnimating = false;
    if (animFrameId) {
      cancelAnimationFrame(animFrameId);
      animFrameId = null;
    }
  }

  let isDriving = false;

  function animate() {
    if (!isAnimating) return;
    animFrameId = requestAnimationFrame(animate);
    // Only render here if NOT driving (driveTick handles rendering when driving)
    if (!isDriving && routeBuilt && renderer && scene && camera) {
      renderer.render(scene, camera);
    }
  }

  function disposeGroup(group) {
    while (group.children.length > 0) {
      const child = group.children[0];
      if (child.geometry) child.geometry.dispose();
      if (child.material) {
        if (child.material.map) child.material.map.dispose();
        child.material.dispose();
      }
      group.remove(child);
    }
  }

  function dispose() {
    stopAnimation();
    if (routeGroup) disposeGroup(routeGroup);
    if (envGroup) disposeGroup(envGroup);
    routeBuilt = false;
  }

  return { init, buildRoute, update, resize, startAnimation, stopAnimation, dispose };
})();
