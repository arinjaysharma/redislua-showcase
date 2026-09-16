function copyApiUrl() {
  const url = window.location.origin + '/api/ping';
  navigator.clipboard.writeText(url).then(() => {
    const textSpan = document.getElementById('copy-text');
    const iconSpan = document.getElementById('copy-icon');
    if (textSpan) textSpan.textContent = 'Copied!';
    if (iconSpan) iconSpan.textContent = '✅';
    setTimeout(() => {
      if (textSpan) textSpan.textContent = 'Copy for Postman / cURL';
      if (iconSpan) iconSpan.textContent = '📋';
    }, 2000);
  }).catch(() => {
    prompt('Copy URL:', url);
  });
}

function updateBannerUrl() {
  const elem = document.getElementById('api-url-display');
  if (elem) {
    elem.textContent = window.location.origin + '/api/ping';
  }
}

// ═══════════════ NAVIGATION ═══════════════
function switchTab(name) {
  document.querySelectorAll('.tab-panel').forEach(p => p.classList.add('hidden'));
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active-tab'));
  const target = document.getElementById('panel-' + name);
  if (target) target.classList.remove('hidden');
  const btn = document.getElementById('tab-btn-' + name);
  if (btn) btn.classList.add('active-tab');
}

// ═══════════════ HELPERS ═══════════════
function logLine(containerId, html) {
  const container = document.getElementById(containerId);
  if (!container) return;
  const d = document.createElement('div');
  d.innerHTML = `<span class="text-gray-600">[${new Date().toLocaleTimeString()}]</span> ${html}`;
  container.prepend(d);
}

// ═══════════════ RATE LIMITER ═══════════════
const RL = {
  updateUI(data) {
    const used = data.limit - (data.remaining !== undefined ? data.remaining : 0);
    const pct = Math.min(100, Math.max(0, (used / data.limit) * 100));
    document.getElementById('rl-used').textContent = used;
    const bar = document.getElementById('rl-bar');
    bar.style.width = pct + '%';
    bar.className = 'h-3 rounded-full transition-all duration-500 ' +
      (pct > 80 ? 'bg-red-500' : pct > 50 ? 'bg-amber-500' : 'bg-emerald-500');

    const badge = document.getElementById('rl-status-badge');
    if (data.remaining === 0) {
      badge.textContent = '⛔ BLOCKED';
      badge.className = 'px-2 py-0.5 rounded-full text-xs bg-red-950 text-red-400 border border-red-800';
    } else {
      badge.textContent = `${data.remaining} remaining`;
      badge.className = 'px-2 py-0.5 rounded-full text-xs bg-emerald-950 text-emerald-400 border border-emerald-800';
    }
    document.getElementById('rl-reset').textContent = data.reset_in_seconds || 60;
  },

  async sendOne() {
    try {
      const res = await fetch('/api/ping');
      const data = await res.json();
      if (res.status === 429) {
        logLine('rl-log', `<span class="text-red-400 font-bold">429 BLOCKED</span> — ${data.message}`);
      } else {
        logLine('rl-log', `<span class="text-emerald-400 font-bold">200 OK</span> — allowed (${data.remaining} remaining)`);
      }
      this.updateUI({ limit: 10, remaining: data.remaining, reset_in_seconds: data.reset_in_seconds });
    } catch (e) {
      logLine('rl-log', `<span class="text-red-500">Error: ${e.message}</span>`);
    }
  },

  async spam() {
    for (let i = 0; i < 20; i++) {
      this.sendOne();
      await new Promise(r => setTimeout(r, 60));
    }
  },

  async runBenchmark() {
    const total = parseInt(document.getElementById('bm-total').value) || 100000;
    const workers = parseInt(document.getElementById('bm-workers').value) || 150;
    const rateLimit = parseInt(document.getElementById('bm-rate').value) || 50000;
    const btn = document.getElementById('bm-btn');
    const stats = document.getElementById('bm-stats');

    btn.disabled = true;
    btn.innerHTML = `<span class="animate-spin inline-block mr-2">⚙️</span> Running 100K Benchmark...`;
    logLine('rl-log', `<span class="text-yellow-400 font-bold">🚀 BENCHMARK STARTED</span> — Firing ${total.toLocaleString()} operations with ${workers} concurrent goroutines...`);

    try {
      const res = await fetch('/api/ratelimit/benchmark', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ total_requests: total, workers: workers, rate_limit: rateLimit })
      });
      const data = await res.json();

      stats.classList.remove('hidden');
      document.getElementById('bm-rps').textContent = (data.rps || 0).toLocaleString() + ' ops/s';
      document.getElementById('bm-allowed').textContent = (data.allowed || 0).toLocaleString();
      document.getElementById('bm-denied').textContent = (data.denied || 0).toLocaleString();
      document.getElementById('bm-duration').textContent = data.duration_ms + ' ms';

      logLine('rl-log', `<span class="text-emerald-400 font-bold">✅ BENCHMARK COMPLETE</span> — <strong>${(data.rps || 0).toLocaleString()} RPS</strong> (${data.allowed.toLocaleString()} allowed, ${data.denied.toLocaleString()} limited in ${data.duration_ms}ms)`);
    } catch (e) {
      logLine('rl-log', `<span class="text-red-500">Benchmark error: ${e.message}</span>`);
    } finally {
      btn.disabled = false;
      btn.innerHTML = `<span>🚀 Run 100,000 RPS Stress Benchmark</span>`;
    }
  },

  async reset() {
    document.getElementById('rl-log').innerHTML = '';
    try {
      const res = await fetch('/api/ratelimit/reset', { method: 'DELETE' });
      const data = await res.json();
      this.updateUI(data);
      logLine('rl-log', `<span class="text-blue-400 font-bold">RESET</span> — Rate limit key cleared. 10 requests available.`);
    } catch (e) {
      logLine('rl-log', `<span class="text-red-500">Reset error: ${e.message}</span>`);
    }
  }
};

// ═══════════════ DISTRIBUTED LOCK ═══════════════
const Lock = {
  tokens: { A: null, B: null },

  async acquire(worker) {
    const resource = document.getElementById('lock-resource').value;
    const ttl = parseInt(document.getElementById('lock-ttl').value);
    try {
      const res = await fetch('/api/lock/acquire', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resource, worker: 'Worker ' + worker, ttl_ms: ttl })
      });
      const data = await res.json();
      if (data.acquired) {
        this.tokens[worker] = data.token;
        this.setWorkerUI(worker, true, data.token);
        logLine('lock-log', `<span class="text-emerald-400 font-bold">ACQUIRED</span> — Worker ${worker} locked <code>${resource}</code> (TTL: ${ttl}ms)`);
        setTimeout(() => {
          if (this.tokens[worker] === data.token) {
            this.setWorkerUI(worker, false, null);
            logLine('lock-log', `<span class="text-gray-500">EXPIRED</span> — Worker ${worker} lock on <code>${resource}</code> expired naturally`);
          }
        }, ttl);
      } else {
        logLine('lock-log', `<span class="text-amber-400 font-bold">REJECTED</span> — Worker ${worker} failed: ${data.message}`);
      }
    } catch (e) {
      logLine('lock-log', `<span class="text-red-500">Error: ${e.message}</span>`);
    }
  },

  async release(worker) {
    const resource = document.getElementById('lock-resource').value;
    const token = this.tokens[worker];
    try {
      const res = await fetch('/api/lock/release', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resource, worker: 'Worker ' + worker, token })
      });
      const data = await res.json();
      if (data.released) {
        this.tokens[worker] = null;
        this.setWorkerUI(worker, false, null);
        logLine('lock-log', `<span class="text-blue-400 font-bold">RELEASED</span> — Worker ${worker} safely released <code>${resource}</code>`);
      } else {
        logLine('lock-log', `<span class="text-red-400 font-bold">RELEASE FAILED</span> — ${data.message}`);
      }
    } catch (e) {
      logLine('lock-log', `<span class="text-red-500">Error: ${e.message}</span>`);
    }
  },

  async race() {
    logLine('lock-log', `<span class="text-yellow-400 font-bold">⚡ RACE TRIGGERED</span> — firing Worker A & B simultaneously...`);
    await Promise.all([this.acquire('A'), this.acquire('B')]);
  },

  setWorkerUI(w, held, token) {
    const lcw = w.toLowerCase();
    const status = document.getElementById(`w${lcw}-status`);
    const tokSpan = document.getElementById(`w${lcw}-token`);
    const acqBtn = document.getElementById(`w${lcw}-acquire`);
    const relBtn = document.getElementById(`w${lcw}-release`);
    if (held) {
      status.textContent = '🔒 locked';
      status.className = 'status-badge status-held';
      tokSpan.textContent = token ? token.substring(0, 12) + '...' : '—';
      acqBtn.disabled = true;
      relBtn.disabled = false;
      acqBtn.classList.add('opacity-50', 'cursor-not-allowed');
      relBtn.classList.remove('opacity-50', 'cursor-not-allowed');
    } else {
      status.textContent = '🔓 free';
      status.className = 'status-badge status-free';
      tokSpan.textContent = '—';
      acqBtn.disabled = false;
      relBtn.disabled = true;
      acqBtn.classList.remove('opacity-50', 'cursor-not-allowed');
      relBtn.classList.add('opacity-50', 'cursor-not-allowed');
    }
  }
};

// ═══════════════ LEADERBOARD ═══════════════
const LB = {
  renderTable(entries) {
    const tbody = document.getElementById('lb-table-body');
    if (!entries || entries.length === 0) {
      tbody.innerHTML = `<tr><td colspan="3" class="px-4 py-8 text-center text-gray-600 text-xs">No data</td></tr>`;
      return;
    }
    tbody.innerHTML = entries.map(e => `
      <tr class="border-b border-gray-800/50 hover:bg-gray-800/30 transition-colors">
        <td class="px-4 py-2.5 font-bold ${e.rank === 1 ? 'text-amber-400' : e.rank === 2 ? 'text-gray-300' : e.rank === 3 ? 'text-amber-600' : 'text-gray-500'}">#${e.rank}</td>
        <td class="px-4 py-2.5 font-medium text-gray-200">${e.player}</td>
        <td class="px-4 py-2.5 text-right font-mono text-emerald-400 font-semibold">${e.score.toLocaleString()}</td>
      </tr>
    `).join('');
  },

  async updateScore() {
    const player = document.getElementById('lb-player').value.trim();
    const score = parseInt(document.getElementById('lb-score').value);
    if (!player || isNaN(score)) return alert('Enter player and score');
    try {
      const res = await fetch('/api/leaderboard/score', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ player, score })
      });
      const data = await res.json();
      this.renderTable(data.top);
      document.getElementById('lb-rank-card').classList.remove('hidden');
      document.getElementById('lb-my-rank').textContent = data.rank;
      document.getElementById('lb-total').textContent = data.total_players;
      document.getElementById('lb-my-score').textContent = data.score.toLocaleString();
      document.getElementById('lb-you-name').textContent = player;
      const above = data.neighbors ? data.neighbors.find(n => n.rank === data.rank - 1) : null;
      const below = data.neighbors ? data.neighbors.find(n => n.rank === data.rank + 1) : null;
      document.getElementById('lb-above').textContent = above ? `${above.player} (${above.score})` : '—';
      document.getElementById('lb-below').textContent = below ? `${below.player} (${below.score})` : '—';
    } catch (e) {
      alert('Error: ' + e.message);
    }
  },

  async seed() {
    await fetch('/api/leaderboard/seed', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ count: 20 }) });
    this.refresh();
  },

  async runBenchmark() {
    const total = parseInt(document.getElementById('bm-total').value) || 100000;
    const workers = parseInt(document.getElementById('bm-workers').value) || 150;
    const rateLimit = parseInt(document.getElementById('bm-rate').value) || 50000;
    const btn = document.getElementById('bm-btn');
    const stats = document.getElementById('bm-stats');

    btn.disabled = true;
    btn.innerHTML = `<span class="animate-spin inline-block mr-2">⚙️</span> Running 100K Benchmark...`;
    logLine('rl-log', `<span class="text-yellow-400 font-bold">🚀 BENCHMARK STARTED</span> — Firing ${total.toLocaleString()} operations with ${workers} concurrent goroutines...`);

    try {
      const res = await fetch('/api/ratelimit/benchmark', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ total_requests: total, workers: workers, rate_limit: rateLimit })
      });
      const data = await res.json();

      stats.classList.remove('hidden');
      document.getElementById('bm-rps').textContent = (data.rps || 0).toLocaleString() + ' ops/s';
      document.getElementById('bm-allowed').textContent = (data.allowed || 0).toLocaleString();
      document.getElementById('bm-denied').textContent = (data.denied || 0).toLocaleString();
      document.getElementById('bm-duration').textContent = data.duration_ms + ' ms';

      logLine('rl-log', `<span class="text-emerald-400 font-bold">✅ BENCHMARK COMPLETE</span> — <strong>${(data.rps || 0).toLocaleString()} RPS</strong> (${data.allowed.toLocaleString()} allowed, ${data.denied.toLocaleString()} limited in ${data.duration_ms}ms)`);
    } catch (e) {
      logLine('rl-log', `<span class="text-red-500">Benchmark error: ${e.message}</span>`);
    } finally {
      btn.disabled = false;
      btn.innerHTML = `<span>🚀 Run 100,000 RPS Stress Benchmark</span>`;
    }
  },

  async reset() {
    await fetch('/api/leaderboard/reset', { method: 'DELETE' });
    document.getElementById('lb-rank-card').classList.add('hidden');
    this.renderTable([]);
  },

  async refresh() {
    const res = await fetch('/api/leaderboard/top?n=10');
    const data = await res.json();
    this.renderTable(data.entries);
  },

  initSSE() {
    const es = new EventSource('/api/leaderboard/stream');
    const dot = document.getElementById('sse-dot');
    const label = document.getElementById('sse-label');
    es.onopen = () => {
      dot.className = 'w-2 h-2 rounded-full bg-emerald-500 animate-pulse';
      label.textContent = 'SSE connected (live)';
      label.className = 'text-xs text-emerald-400';
    };
    es.onerror = () => {
      dot.className = 'w-2 h-2 rounded-full bg-red-500';
      label.textContent = 'SSE reconnecting...';
      label.className = 'text-xs text-red-400';
    };
    es.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        if (payload.top) LB.renderTable(payload.top);
      } catch (_) {}
    };
  }
};

// ═══════════════ FLASH SALE ═══════════════
const FS = {
  maxStock: 10,
  updateStockUI(stock) {
    const num = document.getElementById('fs-stock-num');
    num.textContent = stock >= 0 ? stock : 0;
    num.className = 'text-6xl font-bold tabular-nums transition-all duration-300 ' +
      (stock === 0 ? 'text-red-500' : stock <= 3 ? 'text-amber-400' : 'text-emerald-400');
    const bar = document.getElementById('fs-bar');
    const pct = this.maxStock > 0 ? Math.min(100, (stock / this.maxStock) * 100) : 0;
    bar.style.width = pct + '%';
    bar.className = 'h-4 rounded-full transition-all duration-500 ' +
      (pct === 0 ? 'bg-red-500' : pct < 30 ? 'bg-amber-500' : 'bg-emerald-500');
  },

  async init() {
    const stock = parseInt(document.getElementById('fs-stock-input').value) || 10;
    this.maxStock = stock;
    const res = await fetch('/api/flash-sale/init', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ stock })
    });
    const data = await res.json();
    this.updateStockUI(data.stock);
    document.getElementById('fs-log').innerHTML = '';
    document.getElementById('fs-stats').classList.add('hidden');
    logLine('fs-log', `<span class="text-blue-400 font-bold">INIT</span> — Sale ready with <strong>${stock}</strong> units`);
  },

  async fetchStock() {
    const res = await fetch('/api/flash-sale/stock');
    const data = await res.json();
    this.updateStockUI(data.stock);
  },

  async buyOne() {
    const uid = 'buyer_' + Math.floor(Math.random() * 900 + 100);
    const res = await fetch('/api/flash-sale/buy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ user_id: uid, qty: 1 })
    });
    const data = await res.json();
    if (data.success) {
      this.updateStockUI(data.remaining);
      logLine('fs-log', `<span class="text-emerald-400 font-bold">SUCCESS</span> — <strong>${uid}</strong> bought 1 (${data.remaining} left)`);
    } else {
      logLine('fs-log', `<span class="text-red-400 font-bold">FAILED</span> — <strong>${uid}</strong>: ${data.message}`);
    }
  },

  async stress() {
    const buyers = parseInt(document.getElementById('fs-buyers').value) || 50;
    logLine('fs-log', `<span class="text-yellow-400 font-bold">⚡ STRESS TEST</span> — firing ${buyers} concurrent goroutines...`);
    const res = await fetch('/api/flash-sale/stress', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ buyers })
    });
    const data = await res.json();
    this.fetchStock();

    document.getElementById('fs-stats').classList.remove('hidden');
    document.getElementById('fs-succeeded').textContent = data.succeeded;
    document.getElementById('fs-failed').textContent = data.failed;
    document.getElementById('fs-duration').textContent = data.duration;

    data.purchases.forEach(p => {
      if (p.success) {
        logLine('fs-log', `<span class="text-emerald-400 font-semibold">✅ ${p.user_id}</span> bought 1 unit (${p.remaining} remaining)`);
      } else {
        logLine('fs-log', `<span class="text-gray-500">❌ ${p.user_id}</span> — ${p.message}`);
      }
    });
    logLine('fs-log', `<span class="text-white font-bold">SUMMARY</span>: ${data.succeeded} bought, ${data.failed} rejected in ${data.duration}`);
  }
};

// ═══════════════ WALLET ═══════════════
const Wallet = {
  async fetchBalances() {
    for (const id of ['A', 'B']) {
      const res = await fetch('/api/wallet/balance/' + id);
      const data = await res.json();
      document.getElementById(`wallet-${id.toLowerCase()}-bal`).textContent = data.balance.toLocaleString();
    }
  },

  async topup(id) {
    const amt = parseInt(document.getElementById(`w${id.toLowerCase()}-topup`).value) || 500;
    const res = await fetch('/api/wallet/topup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ wallet_id: id, amount: amt })
    });
    const data = await res.json();
    document.getElementById(`wallet-${id.toLowerCase()}-bal`).textContent = data.balance.toLocaleString();
    logLine('wallet-log', `<span class="text-emerald-400 font-bold">TOP UP</span> — Wallet ${id} credited ₹${amt} (New Balance: ₹${data.balance})`);
  },

  async transfer() {
    const from = document.getElementById('w-from').value;
    const to = document.getElementById('w-to').value;
    const amt = parseInt(document.getElementById('w-amount').value) || 0;
    if (from === to) return alert('Source and destination wallets must differ');
    try {
      const res = await fetch('/api/wallet/transfer', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ from, to, amount: amt })
      });
      const data = await res.json();
      if (data.success) {
        this.fetchBalances();
        logLine('wallet-log', `<span class="text-emerald-400 font-bold">TRANSFER SUCCESS</span> — ₹${amt} from ${from} → ${to} [${data.txn_id}]`);
      } else {
        logLine('wallet-log', `<span class="text-red-400 font-bold">TRANSFER REJECTED</span> — ${data.message} (Wallet ${from} balance: ₹${data.from_balance})`);
      }
    } catch (e) {
      logLine('wallet-log', `<span class="text-red-500">Error: ${e.message}</span>`);
    }
  },

  async overdraft() {
    document.getElementById('w-amount').value = '99999';
    await this.transfer();
  }
};

// ═══════════════ INITIALIZATION ═══════════════
window.addEventListener('DOMContentLoaded', () => {
  updateBannerUrl();
  RL.reset();
  LB.refresh();
  LB.initSSE();
  FS.fetchStock();
  Wallet.fetchBalances();
});
