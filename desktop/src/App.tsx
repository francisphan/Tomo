import { useEffect, useMemo, useState } from 'react';
import {
  search,
  download,
  defaultOutputDir,
  onProgress,
  type SearchHit,
  type SourceResults,
  type ProgressEvent,
} from './api';

type Phase = 'idle' | 'searching' | 'picked' | 'downloading' | 'done';

interface DownloadState {
  seriesTitle: string;
  total: number;
  done: number;
  skipped: number;
  failed: number;
  log: string[];
  interrupted: boolean;
}

const emptyDownload: DownloadState = {
  seriesTitle: '',
  total: 0,
  done: 0,
  skipped: 0,
  failed: 0,
  log: [],
  interrupted: false,
};

export default function App() {
  const [query, setQuery] = useState('');
  const [phase, setPhase] = useState<Phase>('idle');
  const [error, setError] = useState('');
  const [sources, setSources] = useState<SourceResults[]>([]);
  const [picked, setPicked] = useState<SearchHit | null>(null);
  const [chapters, setChapters] = useState('');
  const [out, setOut] = useState('');
  const [dl, setDl] = useState<DownloadState>(emptyDownload);

  useEffect(() => {
    defaultOutputDir().then(setOut).catch(() => {});
  }, []);

  useEffect(() => {
    const un = onProgress(handleProgress);
    return () => {
      un.then((f) => f());
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function handleProgress(e: ProgressEvent) {
    setDl((prev) => {
      const next = { ...prev };
      switch (e.event) {
        case 'series':
          next.seriesTitle = e.title ?? '';
          next.total = e.selected ?? 0;
          break;
        case 'chapter':
          if (e.status === 'ok') next.done += 1;
          else if (e.status === 'skipped') next.skipped += 1;
          else if (e.status === 'failed') next.failed += 1;
          next.log = [
            `${icon(e.status)} Ch. ${e.number ?? '?'}${e.error ? ` — ${e.error}` : ''}`,
            ...next.log,
          ].slice(0, 200);
          break;
        case 'done':
          next.done = e.downloaded ?? next.done;
          next.skipped = e.skipped ?? next.skipped;
          next.failed = e.failed ?? next.failed;
          next.total = e.total ?? next.total;
          next.interrupted = !!e.interrupted;
          break;
      }
      return next;
    });
    if (e.event === 'done') setPhase('done');
  }

  async function runSearch() {
    if (!query.trim()) return;
    setPhase('searching');
    setError('');
    setSources([]);
    setPicked(null);
    try {
      const res = await search(query.trim());
      setSources(res.sources);
      setPhase('idle');
    } catch (err) {
      setError(String(err));
      setPhase('idle');
    }
  }

  function pick(hit: SearchHit) {
    setPicked(hit);
    setPhase('picked');
    setDl(emptyDownload);
  }

  async function runDownload() {
    if (!picked) return;
    setError('');
    setDl(emptyDownload);
    setPhase('downloading');
    try {
      await download(picked.url, chapters.trim(), out.trim());
    } catch (err) {
      setError(String(err));
      setPhase('done');
    }
  }

  const totalHits = useMemo(
    () => sources.reduce((n, s) => n + s.results.length, 0),
    [sources],
  );
  const processed = dl.done + dl.skipped + dl.failed;
  const pct = dl.total > 0 ? Math.round((processed / dl.total) * 100) : 0;

  return (
    <div className="app">
      <header>
        <h1>Tomo</h1>
        <span className="subtitle">manga / manhwa → CBZ for Tachimanga</span>
      </header>

      <section className="search">
        <input
          type="text"
          placeholder="Search a series by title…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && runSearch()}
          autoFocus
        />
        <button onClick={runSearch} disabled={phase === 'searching'}>
          {phase === 'searching' ? 'Searching…' : 'Search'}
        </button>
      </section>

      {error && <div className="error">{error}</div>}

      {sources.length > 0 && (
        <section className="results">
          <div className="muted">{totalHits} result(s)</div>
          {sources.map((s) => (
            <div key={s.source} className="source-block">
              <div className="source-name">{s.source}</div>
              {s.error ? (
                <div className="muted small">search failed: {s.error}</div>
              ) : s.results.length === 0 ? (
                <div className="muted small">no results</div>
              ) : (
                s.results.map((hit) => (
                  <button
                    key={hit.url}
                    className={`hit ${picked?.url === hit.url ? 'selected' : ''}`}
                    onClick={() => pick(hit)}
                  >
                    <span className="hit-title">{hit.title}</span>
                    {hit.info && <span className="hit-info">{hit.info}</span>}
                  </button>
                ))
              )}
            </div>
          ))}
        </section>
      )}

      {picked && (
        <section className="download-panel">
          <div className="picked">
            <strong>{picked.title}</strong>
            <span className="muted small">{picked.url}</span>
          </div>
          <div className="fields">
            <label>
              Chapters
              <input
                type="text"
                placeholder="all (e.g. 1-20,25,40-)"
                value={chapters}
                onChange={(e) => setChapters(e.target.value)}
              />
            </label>
            <label>
              Save to
              <input type="text" value={out} onChange={(e) => setOut(e.target.value)} />
            </label>
          </div>
          <button
            className="primary"
            onClick={runDownload}
            disabled={phase === 'downloading'}
          >
            {phase === 'downloading' ? 'Downloading…' : 'Download'}
          </button>
        </section>
      )}

      {(phase === 'downloading' || phase === 'done') && (
        <section className="progress">
          {dl.seriesTitle && <div className="series">{dl.seriesTitle}</div>}
          <div className="bar">
            <div className="bar-fill" style={{ width: `${pct}%` }} />
          </div>
          <div className="counts">
            {processed}/{dl.total} · {dl.done} downloaded · {dl.skipped} skipped ·{' '}
            {dl.failed} failed
            {phase === 'done' && (dl.interrupted ? ' · interrupted' : ' · done')}
          </div>
          <div className="log">
            {dl.log.map((line, i) => (
              <div key={i} className="log-line">
                {line}
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

function icon(status?: string): string {
  switch (status) {
    case 'ok':
      return '✓';
    case 'skipped':
      return '•';
    case 'failed':
      return '✗';
    default:
      return '·';
  }
}
