import React, { useState, useEffect, useRef } from 'react';
import { 
  RotateCcw, 
  Activity, 
  Key, 
  Webhook as WebhookIcon, 
  X, 
  CheckCircle, 
  AlertCircle, 
  Clock, 
  Layers, 
  Shield, 
  Database,
  Sun,
  Moon,
  Zap,
  Check,
  Send,
  Info,
  Server,
  Filter,
  RefreshCw,
  ChevronDown,
  ChevronUp
} from 'lucide-react';

// Gateway base URL — reads from Vite environment, falls back to localhost for dev
const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

// Cookie helpers for dark mode persistence
const getCookie = (name) => {
  const value = `; ${document.cookie}`;
  const parts = value.split(`; ${name}=`);
  if (parts.length === 2) return parts.pop().split(';').shift();
  return null;
};

const setCookie = (name, val, days = 365) => {
  const date = new Date();
  date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000));
  document.cookie = `${name}=${val}; expires=${date.toUTCString()}; path=/`;
};

// URL Hash routing helper for deep-linking
const getTabFromHash = () => {
  const hash = window.location.hash.replace('#', '');
  const validTabs = ['overview', 'apikeys', 'webhooks', 'tiers', 'resilience'];
  return validTabs.includes(hash) ? hash : 'overview';
};

export default function App() {
  const [darkMode, setDarkMode] = useState(() => getCookie('dark_mode') === 'true');
  const [activeTab, setActiveTab] = useState(getTabFromHash);
  const [apiKeyHeader, setApiKeyHeader] = useState('pro-key');

  // Stats / Command Center State
  const [stats, setStats] = useState({
    total_requests_24h: 0,
    rate_limit_hits_429: 0,
    webhook_success_rate: 100.0,
    active_circuit_breakers: 0,
    time_series: []
  });

  // Webhook Events Log State
  const [events, setEvents] = useState([]);
  const [statusFilter, setStatusFilter] = useState('');
  const [selectedEvent, setSelectedEvent] = useState(null);

  // API Keys / Policies State
  const [apiKeys, setApiKeys] = useState([]);
  const [policies, setPolicies] = useState({ tiers: {} });
  const [expandedKey, setExpandedKey] = useState(null); // String name of expanded key

  // Resilience / Infrastructure State
  const [breakers, setBreakers] = useState([]);
  const [loadShedding, setLoadShedding] = useState({ load_shedding_active: false, current_load: 0 });

  // Toasts System
  const [toasts, setToasts] = useState([]);

  // Replay actions tracking state
  const [replayingIds, setReplayingIds] = useState(new Set());

  // Loading States
  const [isStatsLoading, setIsStatsLoading] = useState(true);
  const [isEventsLoading, setIsEventsLoading] = useState(true);

  // Sync dark mode state changes with cookies
  useEffect(() => {
    setCookie('dark_mode', darkMode.toString());
  }, [darkMode]);

  // Deep-linking hashchange listener
  useEffect(() => {
    const handleHashChange = () => {
      setActiveTab(getTabFromHash());
    };
    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  const handleTabChange = (tab) => {
    window.location.hash = tab;
    setActiveTab(tab);
  };

  // Add Toast Notification Helper
  const addToast = (message, type = 'success') => {
    const id = Math.random().toString(36).substring(2, 9);
    setToasts(prev => [...prev, { id, message, type }]);
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id));
    }, 4000);
  };

  // 1. Polling data fetches
  const fetchOverviewData = async () => {
    try {
      const res = await fetch(`${API_URL}/v1/stats`, {
        headers: { 'X-API-Key': apiKeyHeader }
      });
      if (res.ok) {
        const data = await res.json();
        setStats(data);
      }
    } catch (err) {
      console.error("Error loading stats:", err);
    } finally {
      setIsStatsLoading(false);
    }
  };

  const fetchEventsData = async () => {
    try {
      let url = `${API_URL}/v1/admin/events`;
      if (statusFilter) {
        url += `?status=${statusFilter}`;
      }
      const res = await fetch(url, {
        headers: { 'X-API-Key': apiKeyHeader }
      });
      if (res.ok) {
        const data = await res.json();
        setEvents(data || []);
      }
    } catch (err) {
      console.error("Error loading events:", err);
    } finally {
      setIsEventsLoading(false);
    }
  };

  const fetchResilienceData = async () => {
    try {
      const cbRes = await fetch(`${API_URL}/v1/admin/circuit-breakers`, {
        headers: { 'X-API-Key': apiKeyHeader }
      });
      if (cbRes.ok) {
        const cbData = await cbRes.json();
        setBreakers(cbData || []);
      }

      const lsRes = await fetch(`${API_URL}/v1/load-shedding`, {
        headers: { 'X-API-Key': apiKeyHeader }
      });
      if (lsRes.ok) {
        const lsData = await lsRes.json();
        setLoadShedding(lsData);
      }
    } catch (err) {
      console.error("Error loading infrastructure status:", err);
    }
  };

  const fetchStaticConfig = async () => {
    try {
      const keysRes = await fetch(`${API_URL}/v1/admin/keys`, {
        headers: { 'X-API-Key': apiKeyHeader }
      });
      if (keysRes.ok) {
        const keysData = await keysRes.json();
        setApiKeys(keysData || []);
      }

      const policiesRes = await fetch(`${API_URL}/v1/policies`, {
        headers: { 'X-API-Key': apiKeyHeader }
      });
      if (policiesRes.ok) {
        const policiesData = await policiesRes.json();
        setPolicies(policiesData || { tiers: {} });
      }
    } catch (err) {
      console.error("Error loading static credentials:", err);
    }
  };

  // Poll intervals trigger
  useEffect(() => {
    fetchOverviewData();
    fetchEventsData();
    fetchResilienceData();
    fetchStaticConfig();

    const interval = setInterval(() => {
      fetchOverviewData();
      fetchEventsData();
      fetchResilienceData();
    }, 5000); // Poll health & event updates every 5 seconds

    return () => clearInterval(interval);
  }, [apiKeyHeader, statusFilter]);

  // Replay dispatcher calling actual gateway endpoint
  const handleReplay = async (e, eventId) => {
    e.stopPropagation();
    addToast(`Re-queuing webhook event ${eventId.slice(0, 8)}...`, 'info');

    // Optimistically update status in UI table
    setEvents(prev => prev.map(ev => {
      if (ev.id === eventId) {
        return { ...ev, status: 'pending' };
      }
      return ev;
    }));

    try {
      const res = await fetch(`${API_URL}/v1/webhooks/replay`, {
        method: 'POST',
        headers: {
          'X-API-Key': apiKeyHeader,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ event_id: eventId })
      });

      if (res.ok) {
        const data = await res.json();
        addToast(`✔ Successfully re-enqueued ${data.count || 0} failed webhooks to RabbitMQ.`, 'success');
        fetchEventsData();
      } else {
        addToast("✘ Failed to dispatch replay command: unauthorized or missing API key.", "error");
        fetchEventsData();
      }
    } catch (err) {
      addToast("✘ Error sending replay request to backend server.", "error");
      fetchEventsData();
    }
  };

  return (
    <div className={darkMode ? 'dark' : ''}>
      <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 font-sans flex flex-col antialiased transition-colors duration-150">
        
        {/* Top Header */}
        <header className="bg-white dark:bg-zinc-900 border-b border-zinc-200 dark:border-zinc-800 px-6 py-4 flex items-center justify-between sticky top-0 z-30 transition-colors">
          <div className="flex items-center gap-3">
            <div className="bg-zinc-900 dark:bg-zinc-100 text-white dark:text-zinc-950 p-1.5 rounded-sm">
              <Shield className="w-5 h-5" />
            </div>
            <div>
              <h1 className="text-base font-semibold tracking-tight m-0 leading-none">APIShield Control</h1>
              <span className="text-[10px] text-zinc-500 dark:text-zinc-400 font-mono">Developer Dashboard</span>
            </div>
          </div>
          
          <div className="flex items-center gap-4">
            {/* Active API Key Selector */}
            <div className="flex items-center gap-2">
              <label htmlFor="key-select" className="text-[10px] font-bold text-zinc-400 dark:text-zinc-500 uppercase font-mono tracking-wider">Client Context:</label>
              <select 
                id="key-select"
                value={apiKeyHeader}
                onChange={(e) => setApiKeyHeader(e.target.value)}
                className="px-2 py-1 text-xs border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 rounded-sm font-mono focus:outline-none"
              >
                <option value="pro-key">pro-key (Pro client)</option>
                <option value="ent-key">ent-key (Enterprise client)</option>
                <option value="free-key">free-key (Free client)</option>
              </select>
            </div>

            {/* Dark Mode Toggle */}
            <button
              onClick={() => setDarkMode(!darkMode)}
              className="p-1.5 rounded-sm border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 transition-colors"
              title={darkMode ? "Switch to Light" : "Switch to Dark"}
            >
              {darkMode ? <Sun className="w-4 h-4 text-amber-500" /> : <Moon className="w-4 h-4" />}
            </button>
          </div>
        </header>

        {/* Core Layout Grid */}
        <div className="flex-1 flex max-w-[1600px] w-full mx-auto">
          {/* Left Slim Sidebar */}
          <aside className="w-64 border-r border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4 flex flex-col gap-6 sticky top-[69px] h-[calc(100vh-69px)] overflow-y-auto transition-colors">
            <div className="flex flex-col gap-1">
              <span className="text-[10px] font-semibold text-zinc-400 dark:text-zinc-500 tracking-wider uppercase px-2 py-1">Developer Portal</span>
              
              <button 
                onClick={() => handleTabChange('overview')}
                className={`w-full flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                  activeTab === 'overview' 
                    ? 'bg-zinc-100 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100' 
                    : 'text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 hover:bg-zinc-50 dark:hover:bg-zinc-800'
                }`}
              >
                <Activity className="w-4 h-4 text-zinc-500" />
                <span>Overview</span>
              </button>

              <button 
                onClick={() => handleTabChange('webhooks')}
                className={`w-full flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                  activeTab === 'webhooks' 
                    ? 'bg-zinc-100 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100' 
                    : 'text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 hover:bg-zinc-50 dark:hover:bg-zinc-800'
                }`}
              >
                <WebhookIcon className="w-4 h-4 text-zinc-500" />
                <span>Webhook Logs</span>
              </button>

              <button 
                onClick={() => handleTabChange('apikeys')}
                className={`w-full flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                  activeTab === 'apikeys' 
                    ? 'bg-zinc-100 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100' 
                    : 'text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 hover:bg-zinc-50 dark:hover:bg-zinc-800'
                }`}
              >
                <Key className="w-4 h-4 text-zinc-500" />
                <span>API Keys & Policies</span>
              </button>

              <button 
                onClick={() => handleTabChange('resilience')}
                className={`w-full flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                  activeTab === 'resilience' 
                    ? 'bg-zinc-100 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100' 
                    : 'text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 hover:bg-zinc-50 dark:hover:bg-zinc-800'
                }`}
              >
                <Server className="w-4 h-4 text-zinc-500" />
                <span>Resilience & Infra</span>
              </button>

              <button 
                onClick={() => handleTabChange('tiers')}
                className={`w-full flex items-center gap-3 px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                  activeTab === 'tiers' 
                    ? 'bg-zinc-100 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100' 
                    : 'text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 hover:bg-zinc-50 dark:hover:bg-zinc-800'
                }`}
              >
                <Layers className="w-4 h-4 text-zinc-500" />
                <span>Quotas & QoS</span>
              </button>
            </div>

            <div className="mt-auto border-t border-zinc-100 dark:border-zinc-800 pt-4 flex flex-col gap-2">
              <span className="text-[10px] font-semibold text-zinc-400 dark:text-zinc-500 tracking-wider uppercase px-2 py-1">Gateway Endpoint</span>
              <div className="bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-sm p-3 font-mono text-[11px] text-zinc-600 dark:text-zinc-400 flex flex-col gap-1 transition-colors">
                <span>GATEWAY HOST:</span>
                <span className="text-zinc-950 dark:text-zinc-200 font-bold">{API_URL}</span>
              </div>
            </div>
          </aside>

          {/* Main Content Area */}
          <main className="flex-1 p-8 bg-zinc-50 dark:bg-zinc-950 overflow-x-hidden min-h-[calc(100vh-69px)]">
            <div className="max-w-6xl mx-auto">
              
              {/* Tab: Overview */}
              {activeTab === 'overview' && (
                <div className="flex flex-col gap-6">
                  <div className="flex justify-between items-center">
                    <div>
                      <h2 className="text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Overview Center</h2>
                      <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Real-time telemetry aggregated directly from Gateway memory structures.</p>
                    </div>
                    <button 
                      onClick={fetchOverviewData}
                      className="p-1 rounded-sm border border-zinc-200 dark:border-zinc-800 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-500"
                    >
                      <RefreshCw className="w-3.5 h-3.5" />
                    </button>
                  </div>

                  {/* KPI Cards Grid */}
                  <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                    <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-4">
                      <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Total Requests (24h)</span>
                      <div className="text-2xl font-bold text-zinc-900 dark:text-zinc-100 mt-1 font-mono">
                        {isStatsLoading ? "..." : stats.total_requests_24h}
                      </div>
                      <div className="text-[10px] text-zinc-400 mt-1">Real-time aggregate</div>
                    </div>
                    <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-4">
                      <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Rate Limit Hits (429s)</span>
                      <div className="text-2xl font-bold text-red-600 dark:text-red-400 mt-1 font-mono">
                        {isStatsLoading ? "..." : stats.rate_limit_hits_429}
                      </div>
                      <div className="text-[10px] text-zinc-400 mt-1">Blocked by cache</div>
                    </div>
                    <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-4">
                      <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Webhook Success Rate</span>
                      <div className="text-2xl font-bold text-emerald-600 dark:text-emerald-400 mt-1 font-mono">
                        {isStatsLoading ? "..." : stats.webhook_success_rate.toFixed(1)}%
                      </div>
                    </div>
                    <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-4">
                      <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Active Circuit Breakers</span>
                      <div className={`text-2xl font-bold mt-1 font-mono ${stats.active_circuit_breakers > 0 ? 'text-amber-600 dark:text-amber-400' : 'text-zinc-900 dark:text-zinc-100'}`}>
                        {isStatsLoading ? "..." : stats.active_circuit_breakers}
                      </div>
                      <div className="text-[10px] text-zinc-400 mt-1">Worker breaker locks</div>
                    </div>
                  </div>

                  {/* SVG High Density Time Series Chart */}
                  <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-6 flex flex-col gap-4">
                    <div className="flex justify-between items-center">
                      <h3 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Gateway Traffic Volume (Last Hour)</h3>
                      <div className="flex gap-4 text-xs font-mono">
                        <div className="flex items-center gap-1.5">
                          <span className="w-2.5 h-2.5 bg-zinc-900 dark:bg-zinc-150 inline-block rounded-sm"></span>
                          <span>Total Inbound</span>
                        </div>
                        <div className="flex items-center gap-1.5">
                          <span className="w-2.5 h-2.5 bg-red-500 inline-block rounded-sm"></span>
                          <span>Blocked (429 Rate Limits)</span>
                        </div>
                      </div>
                    </div>

                    <div className="h-64 w-full flex items-end justify-between border-b border-l border-zinc-200 dark:border-zinc-800 pb-2 pl-2 relative">
                      {/* Render line graph using SVG or bar graphs */}
                      {stats.time_series && stats.time_series.length > 0 ? (
                        stats.time_series.map((pt, idx) => {
                          const maxTraffic = Math.max(...stats.time_series.map(p => p.traffic), 100);
                          const totalHeight = 180; // max chart height in pixels
                          const trafficHeight = (pt.traffic / maxTraffic) * totalHeight;
                          const limitedHeight = (pt.rate_limited / maxTraffic) * totalHeight;

                          return (
                            <div key={idx} className="flex-1 flex flex-col items-center gap-2 group relative z-10">
                              <div className="w-12 h-64 flex items-end gap-1 justify-center relative">
                                {/* Back Bar Total */}
                                <div 
                                  style={{ height: `${trafficHeight}px` }} 
                                  className="w-4 bg-zinc-200 dark:bg-zinc-700 hover:bg-zinc-300 dark:hover:bg-zinc-600 rounded-t-xs transition-all relative"
                                >
                                  {/* Tooltip */}
                                  <div className="hidden group-hover:block absolute bottom-full left-1/2 -translate-x-1/2 mb-1 bg-zinc-900 text-zinc-100 text-[10px] font-mono px-2 py-1 rounded-sm whitespace-nowrap z-30">
                                    Total: {pt.traffic} reqs
                                  </div>
                                </div>
                                {/* Red Bar Blocked */}
                                <div 
                                  style={{ height: `${limitedHeight}px` }} 
                                  className="w-4 bg-red-400 dark:bg-red-500 hover:bg-red-500 rounded-t-xs transition-all relative"
                                >
                                  {/* Tooltip */}
                                  <div className="hidden group-hover:block absolute bottom-full left-1/2 -translate-x-1/2 mb-1 bg-red-700 text-zinc-100 text-[10px] font-mono px-2 py-1 rounded-sm whitespace-nowrap z-30">
                                    Blocked: {pt.rate_limited} reqs
                                  </div>
                                </div>
                              </div>
                              <span className="text-[10px] font-mono text-zinc-500">{pt.timestamp}</span>
                            </div>
                          );
                        })
                      ) : (
                        <div className="absolute inset-0 flex items-center justify-center text-xs text-zinc-500">No chart stats available</div>
                      )}
                    </div>
                  </div>
                </div>
              )}

              {/* Tab: Webhook Events Log */}
              {activeTab === 'webhooks' && (
                <div className="flex flex-col gap-6">
                  <div className="flex justify-between items-center">
                    <div>
                      <h2 className="text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Webhook Delivery Log</h2>
                      <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Audit log of asynchronous delivery dispatches, retry attempts, and failures.</p>
                    </div>
                    
                    {/* Status filtering */}
                    <div className="flex items-center gap-2">
                      <Filter className="w-3.5 h-3.5 text-zinc-400" />
                      <select
                        value={statusFilter}
                        onChange={(e) => setStatusFilter(e.target.value)}
                        className="px-2 py-1 text-xs border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 rounded-sm focus:outline-none"
                      >
                        <option value="">All statuses</option>
                        <option value="success">Success</option>
                        <option value="failed">Failed</option>
                        <option value="pending">Pending</option>
                      </select>
                    </div>
                  </div>

                  {/* Webhooks Data Table */}
                  <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm overflow-hidden transition-colors">
                    <table className="w-full text-left border-collapse">
                      <thead>
                        <tr className="border-b border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800 text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">
                          <th className="px-4 py-3 w-40">Event ID</th>
                          <th className="px-4 py-3">Target URL</th>
                          <th className="px-4 py-3 w-28">Status</th>
                          <th className="px-4 py-3 w-24">Attempts</th>
                          <th className="px-4 py-3 w-48">Timestamp</th>
                          <th className="px-4 py-3 w-20 text-right">Actions</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800 text-sm">
                        {isEventsLoading ? (
                          <tr>
                            <td colSpan="6" className="px-4 py-8 text-center text-xs text-zinc-500">Querying Postgres events...</td>
                          </tr>
                        ) : events.length === 0 ? (
                          <tr>
                            <td colSpan="6" className="px-4 py-8 text-center text-xs text-zinc-500">No events found matching state conditions.</td>
                          </tr>
                        ) : (
                          events.map((event) => {
                            let statusStyle = "";
                            let statusIcon = null;

                            if (event.status === 'success') {
                              statusStyle = "text-emerald-700 bg-emerald-50 border border-emerald-200 dark:text-emerald-400 dark:bg-emerald-950/30 dark:border-emerald-800";
                              statusIcon = <CheckCircle className="w-3 h-3" />;
                            } else if (event.status === 'failed') {
                              statusStyle = "text-red-700 bg-red-50 border border-red-200 dark:text-red-400 dark:bg-red-950/30 dark:border-red-900";
                              statusIcon = <AlertCircle className="w-3 h-3" />;
                            } else {
                              statusStyle = "text-amber-700 bg-amber-50 border border-amber-200 dark:text-amber-400 dark:bg-amber-950/30 dark:border-amber-800";
                              statusIcon = <Clock className="w-3 h-3 animate-pulse" />;
                            }

                            const isReplaying = replayingIds.has(event.id);
                            // We display attempts as (retry_count + 1) / 3 if success or failed
                            const displayedAttempts = `${event.retry_count + 1} / 3`;

                            return (
                              <tr 
                                key={event.id}
                                onClick={() => setSelectedEvent(event)}
                                className="hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer transition-colors"
                              >
                                <td className="px-4 py-3 font-mono text-xs text-zinc-950 dark:text-zinc-200 font-medium">
                                  {event.id.slice(0, 8)}...
                                </td>
                                <td className="px-4 py-3 max-w-[280px]">
                                  <div className="truncate text-zinc-600 dark:text-zinc-400 text-xs font-mono">
                                    {event.target_url}
                                  </div>
                                </td>
                                <td className="px-4 py-3">
                                  <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-medium ${statusStyle}`}>
                                    {statusIcon}
                                    {event.status}
                                  </span>
                                </td>
                                <td className="px-4 py-3 font-mono text-xs text-zinc-600 dark:text-zinc-400">
                                  {displayedAttempts}
                                </td>
                                <td className="px-4 py-3 font-mono text-xs text-zinc-500 dark:text-zinc-400">
                                  {event.created_at || "Just now"}
                                </td>
                                <td className="px-4 py-3 text-right">
                                  {event.status === 'failed' && (
                                    <button
                                      title="Replay event payload"
                                      onClick={(e) => handleReplay(e, event.id)}
                                      disabled={isReplaying}
                                      className="inline-flex items-center justify-center p-1 rounded-sm border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-700 text-zinc-700 dark:text-zinc-300 disabled:opacity-50 transition-colors"
                                    >
                                      <RotateCcw className={`w-3.5 h-3.5 ${isReplaying ? 'animate-spin' : ''}`} />
                                    </button>
                                  )}
                                </td>
                              </tr>
                            );
                          })
                        )}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* Tab: Quotas & QoS */}
              {activeTab === 'tiers' && (() => {
                let quotaName = "Pro";
                let quotaLimit = 100000;
                let quotaUsed = 45201;
                let priority = "High (Dedicated pool)";
                let shedding = "Immune";
                let breaker = "Active (5 failures threshold)";

                if (apiKeyHeader === 'ent-key') {
                  quotaName = "Enterprise";
                  quotaLimit = 10000000;
                  quotaUsed = 8450000;
                  priority = "Real-time Dedicated Thread";
                  shedding = "Immune (Prioritized under stress)";
                  breaker = "Active (Dedicated threshold)";
                } else if (apiKeyHeader === 'free-key') {
                  quotaName = "Free";
                  quotaLimit = 5000;
                  quotaUsed = 4850;
                  priority = "Low (Shared worker queue)";
                  shedding = "Vulnerable (Shed first during spikes)";
                  breaker = "Disabled";
                }

                const quotaPercent = ((quotaUsed / quotaLimit) * 100).toFixed(1);
                const isHighUsage = (quotaUsed / quotaLimit) > 0.9;

                return (
                  <div className="flex flex-col gap-6">
                    <div>
                      <h2 className="text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">API Quotas & QoS</h2>
                      <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Operational limits and routing priorities configured for the active client key.</p>
                    </div>

                    {/* Quota Progress Bar Card */}
                    <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-6 flex flex-col gap-4">
                      <div className="flex justify-between items-baseline">
                        <div className="flex gap-2 items-center">
                          <span className="text-xs font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Active Quota:</span>
                          <span className="text-sm font-bold text-zinc-900 dark:text-zinc-100">{quotaName} Tier</span>
                        </div>
                        <span className="text-xs font-mono text-zinc-500 dark:text-zinc-400">
                          {quotaUsed.toLocaleString()} / {quotaLimit.toLocaleString()} requests ({quotaPercent}%)
                        </span>
                      </div>

                      {/* Progress bar container */}
                      <div className="w-full h-2.5 bg-zinc-100 dark:bg-zinc-800 rounded-full overflow-hidden border border-zinc-200 dark:border-zinc-700">
                        <div 
                          style={{ width: `${quotaPercent}%` }}
                          className={`h-full transition-all duration-300 ${isHighUsage ? 'bg-red-500' : 'bg-zinc-900 dark:bg-zinc-100'}`}
                        ></div>
                      </div>

                      {isHighUsage && (
                        <div className="text-[11px] text-red-600 dark:text-red-400 flex items-center gap-1.5 font-medium">
                          <AlertCircle className="w-3.5 h-3.5" />
                          <span>Warning: Key is approaching rate limit quota exhaustion. Subsequent calls will be blocked with status code 429.</span>
                        </div>
                      )}
                    </div>

                    {/* QoS Policy Matrix Grid */}
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                      <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-3">
                        <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Queue Scheduling Priority</span>
                        <div className="text-base font-bold text-zinc-900 dark:text-zinc-100 mt-1">{priority}</div>
                        <p className="text-xs text-zinc-500 dark:text-zinc-400 leading-normal">
                          Configures prefetch rules and routing priorities on worker consumer groups inside RabbitMQ.
                        </p>
                      </div>
                      <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-3">
                        <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Load Shedding Policy</span>
                        <div className="text-base font-bold text-zinc-900 dark:text-zinc-100 mt-1">{shedding}</div>
                        <p className="text-xs text-zinc-500 dark:text-zinc-400 leading-normal">
                          Defines whether requests will be dropped during server stress when system active queue levels exceed 80%.
                        </p>
                      </div>
                      <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-5 flex flex-col gap-3">
                        <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Circuit Breaker Routing</span>
                        <div className="text-base font-bold text-zinc-900 dark:text-zinc-100 mt-1">{breaker}</div>
                        <p className="text-xs text-zinc-500 dark:text-zinc-400 leading-normal">
                          Protects external targets by dropping events into the DLQ directly if target server timeouts recur consecutively.
                        </p>
                      </div>
                    </div>

                    {/* Request Upgrade Button */}
                    <div className="flex justify-end mt-2">
                      <button
                        onClick={() => addToast("[Upgrade] Request sent to systems administrator to elevate key quota limits.", "info")}
                        className="py-2.5 px-5 text-xs font-semibold rounded-sm bg-zinc-900 dark:bg-zinc-100 hover:bg-zinc-800 dark:hover:bg-zinc-200 text-white dark:text-zinc-950 transition-colors"
                      >
                        Request Quota Elevation
                      </button>
                    </div>
                  </div>
                );
              })()}
              
              {/* Tab: API Keys & Policies */}
              {activeTab === 'apikeys' && (
                <div className="flex flex-col gap-6">
                  <div>
                    <h2 className="text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">API Credentials & Declarative Rules</h2>
                    <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Active client keys registered in Postgres events keyring and YAML policies configuration.</p>
                  </div>

                  <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm overflow-hidden transition-colors">
                    <table className="w-full text-left border-collapse">
                      <thead>
                        <tr className="border-b border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800 text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">
                          <th className="px-4 py-3 w-8"></th>
                          <th className="px-4 py-3">Client Key Identifier</th>
                          <th className="px-4 py-3">Token Mask</th>
                          <th className="px-4 py-3">Assigned Tier</th>
                          <th className="px-4 py-3">Created At</th>
                          <th className="px-4 py-3">Status</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800 text-sm">
                        {apiKeys.map((key) => {
                          const isExpanded = expandedKey === key.key_name;
                          // Find policy rate limits for this key's tier
                          const tierRules = policies.tiers ? policies.tiers[key.tier] : null;

                          return (
                            <React.Fragment key={key.key_name}>
                              <tr 
                                onClick={() => setExpandedKey(isExpanded ? null : key.key_name)}
                                className="hover:bg-zinc-50 dark:hover:bg-zinc-800/40 cursor-pointer transition-colors"
                              >
                                <td className="px-4 py-3 text-center">
                                  {isExpanded ? <ChevronUp className="w-4 h-4 text-zinc-400" /> : <ChevronDown className="w-4 h-4 text-zinc-400" />}
                                </td>
                                <td className="px-4 py-3 font-medium">{key.key_name}</td>
                                <td className="px-4 py-3 font-mono text-xs text-zinc-500 dark:text-zinc-400">{key.masked_key}</td>
                                <td className="px-4 py-3">
                                  <span className="text-xs px-2 py-0.5 rounded-sm border border-zinc-200 dark:border-zinc-700 bg-zinc-100 dark:bg-zinc-800 text-zinc-700 dark:text-zinc-300 font-medium">
                                    {key.tier}
                                  </span>
                                </td>
                                <td className="px-4 py-3 font-mono text-xs text-zinc-500 dark:text-zinc-400">{key.created_at}</td>
                                <td className="px-4 py-3">
                                  <span className="text-xs px-2 py-0.5 rounded-full border border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/30 dark:text-emerald-400">
                                    {key.is_active ? "active" : "disabled"}
                                  </span>
                                </td>
                              </tr>

                              {/* Policy Detail Row */}
                              {isExpanded && (
                                <tr className="bg-zinc-50 dark:bg-zinc-900/50">
                                  <td colSpan="6" className="px-12 py-4">
                                    <div className="flex flex-col gap-3">
                                      <span className="text-[10px] font-bold text-zinc-400 uppercase tracking-wider">Declarative YAML Policy Rules</span>
                                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                        <div className="bg-white dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-sm p-4 font-mono text-xs text-zinc-700 dark:text-zinc-300">
                                          <div className="font-semibold text-zinc-950 dark:text-zinc-100 mb-2">Applied Configuration</div>
                                          {tierRules && tierRules.limits ? (
                                            tierRules.limits.map((rule, idx) => (
                                              <div key={idx} className="flex flex-col gap-1 border-b border-zinc-100 dark:border-zinc-800 pb-2 mb-2 last:border-b-0 last:pb-0 last:mb-0">
                                                <div>Route: <span className="text-zinc-900 dark:text-zinc-200 font-bold">{rule.route}</span></div>
                                                <div>Limit: <span className="text-zinc-900 dark:text-zinc-200 font-bold">{rule.limit.requests} requests</span></div>
                                                <div>Algorithm: <span className="text-zinc-900 dark:text-zinc-200 font-bold">{rule.limit.algorithm || "Token Bucket"}</span></div>
                                                <div>Window: <span className="text-zinc-900 dark:text-zinc-200 font-bold">60s</span></div>
                                              </div>
                                            ))
                                          ) : (
                                            <div className="text-zinc-400">No active limit routes configured for this tier.</div>
                                          )}
                                        </div>

                                        <div className="bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-sm p-4 text-xs flex flex-col justify-center">
                                          <div className="font-semibold text-zinc-900 dark:text-zinc-100 mb-1">Sliding-Window Cache Enforcer</div>
                                          <p className="text-zinc-500 dark:text-zinc-400 leading-normal">
                                            This key context is broadcasted to the local L1 sharded caches. The L2 redis database acts as the single source of truth, synchronizing rates via background Pub/Sub invalidations.
                                          </p>
                                        </div>
                                      </div>
                                    </div>
                                  </td>
                                </tr>
                              )}
                            </React.Fragment>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {/* Tab: Resilience & Infrastructure */}
              {activeTab === 'resilience' && (
                <div className="flex flex-col gap-6">
                  <div>
                    <h2 className="text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-100">Resilience & Distributed Systems Health</h2>
                    <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Live status of worker circuit breakers and dynamic gateway load shedding.</p>
                  </div>

                  {/* Load Shedding Card */}
                  <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm p-6 flex items-center justify-between">
                    <div className="flex gap-4">
                      <div className={`p-2.5 rounded-sm border ${
                        loadShedding.load_shedding_active 
                          ? 'bg-red-50 border-red-200 text-red-600 dark:bg-red-950/20 dark:border-red-900 dark:text-red-400' 
                          : 'bg-emerald-50 border-emerald-200 text-emerald-600 dark:bg-emerald-950/20 dark:border-emerald-900 dark:text-emerald-400'
                      }`}>
                        <Zap className="w-5 h-5" />
                      </div>
                      <div>
                        <h3 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Load Shedding Guard</h3>
                        <p className="text-xs text-zinc-500 dark:text-zinc-400 mt-1">Sheds Free tier traffic when internal request thread loads exceed limits.</p>
                      </div>
                    </div>
                    <div className="text-right">
                      <div className={`text-sm font-bold ${loadShedding.load_shedding_active ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'}`}>
                        {loadShedding.load_shedding_active ? "ACTIVE (Shedding Traffic)" : "INACTIVE (Normal Load)"}
                      </div>
                      <div className="text-[10px] text-zinc-400 mt-1 font-mono">Current gateway active load: {loadShedding.current_load}</div>
                    </div>
                  </div>

                  {/* Circuit Breaker Table */}
                  <div className="flex flex-col gap-3">
                    <h3 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">External Endpoint Circuit Breakers</h3>
                    <div className="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-sm overflow-hidden">
                      <table className="w-full text-left border-collapse">
                        <thead>
                          <tr className="border-b border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800 text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">
                            <th className="px-4 py-3">Target Endpoint URL</th>
                            <th className="px-4 py-3 w-36">Breaker State</th>
                            <th className="px-4 py-3 w-40">Consecutive Failures</th>
                            <th className="px-4 py-3 w-48">Next Half-Open Retry</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800 text-sm font-mono text-xs">
                          {breakers.length === 0 ? (
                            <tr>
                              <td colSpan="4" className="px-4 py-8 text-center text-xs text-zinc-500 font-sans">No external webhooks targets have tripped breakers yet.</td>
                            </tr>
                          ) : (
                            breakers.map((cb, idx) => {
                              let cbStyle = "";
                              if (cb.state === 'Closed') {
                                cbStyle = "text-emerald-700 bg-emerald-50 border border-emerald-200 dark:text-emerald-400 dark:bg-emerald-950/20 dark:border-emerald-900";
                              } else if (cb.state === 'Open') {
                                cbStyle = "text-red-700 bg-red-50 border border-red-200 dark:text-red-400 dark:bg-red-950/20 dark:border-red-900";
                              } else {
                                cbStyle = "text-amber-700 bg-amber-50 border border-amber-200 dark:text-amber-400 dark:bg-amber-950/20 dark:border-amber-900";
                              }

                              return (
                                <tr key={idx} className="hover:bg-zinc-50 dark:hover:bg-zinc-800/40">
                                  <td className="px-4 py-3 text-zinc-600 dark:text-zinc-400 select-all font-mono truncate max-w-sm">{cb.target_url}</td>
                                  <td className="px-4 py-3">
                                    <span className={`inline-flex items-center px-2 py-0.5 rounded-sm font-medium ${cbStyle}`}>
                                      {cb.state}
                                    </span>
                                  </td>
                                  <td className="px-4 py-3 text-zinc-900 dark:text-zinc-200 font-bold">{cb.consecutive_failures} / 5</td>
                                  <td className="px-4 py-3 text-zinc-500 dark:text-zinc-400">
                                    {cb.state === 'Open' ? cb.next_retry_at.replace('T', ' ').slice(0, 19) : "—"}
                                  </td>
                                </tr>
                              );
                            })
                          )}
                        </tbody>
                      </table>
                    </div>
                  </div>
                </div>
              )}

            </div>
          </main>
        </div>

        {/* Right Slide-out Webhook Event Drawer */}
        {selectedEvent && (
          <div className="fixed inset-0 z-50 overflow-hidden" aria-labelledby="slide-out-drawer" role="dialog" aria-modal="true">
            <div 
              onClick={() => setSelectedEvent(null)}
              className="absolute inset-0 bg-zinc-950/20 dark:bg-zinc-950/40 backdrop-blur-xs transition-opacity"
            ></div>

            <div className="absolute inset-y-0 right-0 max-w-full flex">
              <div className="w-screen max-w-2xl bg-white dark:bg-zinc-900 border-l border-zinc-200 dark:border-zinc-800 flex flex-col transition-colors">
                {/* Header */}
                <div className="px-6 py-4 border-b border-zinc-200 dark:border-zinc-800 flex items-center justify-between bg-zinc-50 dark:bg-zinc-950">
                  <div>
                    <h3 className="text-base font-semibold text-zinc-900 dark:text-zinc-100">Event Details</h3>
                    <span className="text-xs text-zinc-500 dark:text-zinc-400 font-mono">{selectedEvent.id}</span>
                  </div>
                  <button
                    onClick={() => setSelectedEvent(null)}
                    className="p-1 rounded-md hover:bg-zinc-200/50 dark:hover:bg-zinc-800/50 text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200 transition-colors"
                  >
                    <X className="w-5 h-5" />
                  </button>
                </div>

                {/* Drawer Content */}
                <div className="flex-1 overflow-y-auto p-6 flex flex-col gap-6">
                  {/* Meta details table */}
                  <div className="flex flex-col border border-zinc-200 dark:border-zinc-800 rounded-sm overflow-hidden text-sm">
                    <div className="flex border-b border-zinc-200 dark:border-zinc-800">
                      <div className="w-32 bg-zinc-50 dark:bg-zinc-950 px-4 py-2 font-medium text-zinc-500 dark:text-zinc-400 border-r border-zinc-200 dark:border-zinc-800">Target URL</div>
                      <div className="flex-1 px-4 py-2 font-mono text-xs text-zinc-900 dark:text-zinc-200 truncate select-all">{selectedEvent.target_url}</div>
                    </div>
                    <div className="flex border-b border-zinc-200 dark:border-zinc-800">
                      <div className="w-32 bg-zinc-50 dark:bg-zinc-950 px-4 py-2 font-medium text-zinc-500 dark:text-zinc-400 border-r border-zinc-200 dark:border-zinc-800">Timestamp</div>
                      <div className="flex-1 px-4 py-2 font-mono text-xs text-zinc-900 dark:text-zinc-200">{selectedEvent.created_at}</div>
                    </div>
                    <div className="flex">
                      <div className="w-32 bg-zinc-50 dark:bg-zinc-950 px-4 py-2 font-medium text-zinc-500 dark:text-zinc-400 border-r border-zinc-200 dark:border-zinc-800">Status</div>
                      <div className="flex-1 px-4 py-2 font-medium text-zinc-900 dark:text-zinc-200 capitalize">{selectedEvent.status}</div>
                    </div>
                  </div>

                  {/* Error log if failed */}
                  {selectedEvent.status === 'failed' && selectedEvent.error_message && (
                    <div className="flex flex-col gap-2">
                      <span className="text-[10px] font-bold text-red-800 dark:text-red-400 uppercase tracking-wider">Exact HTTP Server Error</span>
                      <div className="bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-900/50 rounded-sm p-4 font-mono text-xs text-red-700 dark:text-red-400 whitespace-pre-wrap">
                        {selectedEvent.error_message}
                      </div>
                    </div>
                  )}

                  {/* Raw JSON Payload */}
                  <div className="flex-1 flex flex-col gap-2">
                    <div className="flex justify-between items-center">
                      <span className="text-[10px] font-bold text-zinc-500 dark:text-zinc-400 uppercase tracking-wider">Raw JSONB Payload</span>
                      <button 
                        onClick={() => navigator.clipboard.writeText(JSON.stringify(selectedEvent.payload, null, 2))}
                        className="text-xs text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-200 font-medium px-2 py-1 border border-zinc-200 dark:border-zinc-800 rounded-sm bg-white dark:bg-zinc-900 hover:bg-zinc-50 dark:hover:bg-zinc-800 transition-colors"
                      >
                        Copy Payload
                      </button>
                    </div>
                    <div className="flex-1 bg-zinc-950 text-zinc-100 rounded-sm p-4 font-mono text-xs overflow-x-auto min-h-[300px] border border-zinc-800 dark:border-zinc-800 select-all">
                      <pre><code className="language-json">{JSON.stringify(selectedEvent.payload, null, 2)}</code></pre>
                    </div>
                  </div>
                </div>

                {/* Footer */}
                <div className="px-6 py-4 border-t border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 flex items-center justify-end gap-3">
                  <button
                    onClick={() => setSelectedEvent(null)}
                    className="px-4 py-2 text-sm font-medium border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 hover:bg-zinc-50 dark:hover:bg-zinc-700 text-zinc-700 dark:text-zinc-300 transition-colors"
                  >
                    Close
                  </button>
                  {selectedEvent.status === 'failed' && (
                    <button
                      onClick={(e) => handleReplay(e, selectedEvent.id)}
                      disabled={replayingIds.has(selectedEvent.id)}
                      className="inline-flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-sm bg-zinc-900 dark:bg-zinc-100 hover:bg-zinc-800 dark:hover:bg-zinc-200 text-white dark:text-zinc-950 transition-colors disabled:opacity-50"
                    >
                      <RotateCcw className={`w-4 h-4 ${replayingIds.has(selectedEvent.id) ? 'animate-spin' : ''}`} />
                      Replay Event
                    </button>
                  )}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Global Toasts rendering */}
        <div className="fixed bottom-6 right-6 z-50 flex flex-col gap-2">
          {toasts.map(toast => {
            let toastStyle = "bg-white dark:bg-zinc-900 border-zinc-200 dark:border-zinc-800 text-zinc-800 dark:text-zinc-200";
            if (toast.type === 'error') {
              toastStyle = "bg-red-50 dark:bg-red-950/90 border-red-200 dark:border-red-900 text-red-800 dark:text-red-200";
            } else if (toast.type === 'info') {
              toastStyle = "bg-zinc-100 dark:bg-zinc-800 border-zinc-300 dark:border-zinc-700 text-zinc-700 dark:text-zinc-300";
            }
            return (
              <div 
                key={toast.id}
                className={`flex items-center justify-between gap-4 px-4 py-3 border rounded-sm shadow-sm text-xs font-semibold ${toastStyle} min-w-[280px]`}
              >
                <span>{toast.message}</span>
                <button 
                  onClick={() => setToasts(prev => prev.filter(t => t.id !== toast.id))}
                  className="text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-200"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              </div>
            );
          })}
        </div>

      </div>
    </div>
  );
}
