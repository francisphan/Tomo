// Thin wrappers around the Tauri commands exposed by the Rust shell, which in
// turn drive the bundled `tomo` CLI sidecar.
import { invoke } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';

export interface SearchHit {
  title: string;
  url: string;
  source: string;
  info?: string;
}

export interface SourceResults {
  source: string;
  error?: string;
  results: SearchHit[];
}

export interface SearchResponse {
  query: string;
  sources: SourceResults[];
}

/** A progress event emitted while a download runs (mirrors the CLI's NDJSON). */
export interface ProgressEvent {
  event: 'fetching' | 'series' | 'saving' | 'chapters' | 'chapter' | 'done';
  source?: string;
  title?: string;
  found?: number;
  selected?: number;
  dir?: string;
  number?: string;
  status?: 'ok' | 'skipped' | 'failed' | 'canceled';
  error?: string;
  downloaded?: number;
  skipped?: number;
  failed?: number;
  total?: number;
  interrupted?: boolean;
}

export function search(query: string, source = '', limit = 10): Promise<SearchResponse> {
  return invoke('search', { query, source, limit });
}

export function download(url: string, chapters: string, out: string): Promise<void> {
  return invoke('download', { url, chapters, out });
}

export function defaultOutputDir(): Promise<string> {
  return invoke('default_output_dir');
}

export function onProgress(cb: (e: ProgressEvent) => void): Promise<UnlistenFn> {
  return listen<ProgressEvent>('tomo://progress', (event) => cb(event.payload));
}
