function $(id) { return document.getElementById(id); }

function icon(ok) { return ok ? '🟢' : '⚫'; }

async function poll() {
  try {
    const r = await fetch('/api/status');
    const data = await r.json();
    updateStatus(data);
  } catch(e) {
    $('tag-status').className = 'tag warn';
    $('tag-status').textContent = 'offline';
  }
}

function updateStatus(data) {
  if (!data || !data.tools) return;
  $('tag-status').className = 'tag ok';
  $('tag-status').textContent = 'online';

  for (const tool of data.tools) {
    switch(tool.name) {
      case 'codebase-memory-mcp':
        $('dot-cbmcp').className = 'status-dot ' + (tool.healthy ? 'online' : 'offline');
        $('cbmcp-status').textContent = tool.healthy ? (tool.details || 'Conectado') : 'Offline';
        break;
      case 'rtk':
        $('dot-rtk').className = 'status-dot ' + (tool.healthy ? 'online' : 'offline');
        break;
      case 'headroom':
        $('dot-headroom').className = 'status-dot ' + (tool.healthy ? 'online' : 'offline');
        $('hr-status').textContent = tool.healthy ? 'ONLINE' : 'OFFLINE';
        $('hr-port').textContent = tool.healthy ? 'port 8787' : 'offline';
        break;
      case 'memstack':
        $('dot-memstack').className = 'status-dot ' + (tool.healthy ? 'online' : 'offline');
        $('ms-status').textContent = tool.healthy ? 'Disponível' : 'Indisponível';
        break;
    }
  }
}

function updateRTK(data) {
  if (!data) return;
  $('rtk-tokens').textContent = formatTokens(data.tokens_saved || 0);
  $('rtk-pct').textContent = (data.pct_saved || 0).toFixed(1) + '%';
  $('rtk-bar').style.width = (data.pct_saved || 0) + '%';
}

function updateMetrics(data) {
  if (data && data.rtk) updateRTK(data.rtk);
}

function formatTokens(n) {
  if (n >= 1_000_000) return (n/1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n/1000).toFixed(0) + 'K';
  return String(n);
}

async function indexRepo() {
  const path = $('index-path').value;
  if (!path) return;
  const r = await fetch('/api/codebase/index', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({path})
  });
  const d = await r.json();
  alert(JSON.stringify(d, null, 2));
  poll();
}

async function searchMemstack() {
  const q = $('ms-query').value;
  if (!q) return;
  const r = await fetch('/api/memstack/search', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({query: q})
  });
  const d = await r.json();
  $('ms-results').textContent = d.results || 'sem resultados';
}

// SSE
let evtSource = null;
function connectSSE() {
  if (evtSource) evtSource.close();
  evtSource = new EventSource('/api/events');
  evtSource.addEventListener('status', (e) => {
    try { updateStatus(JSON.parse(e.data)); } catch(ex) {}
  });
  evtSource.onerror = () => {
    $('tag-status').className = 'tag warn';
    $('tag-status').textContent = 'reconnecting';
    setTimeout(connectSSE, 3000);
  };
}

poll();
connectSSE();
setInterval(() => fetch('/api/rtk/gain').then(r=>r.json()).then(d=>updateRTK(d)).catch(()=>{}), 10000);
