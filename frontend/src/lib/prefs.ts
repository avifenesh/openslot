import { writable, get } from 'svelte/store';
import type { User } from './api';

export interface UserPrefs {
	timezone: string;
	time_format: '12h' | '24h';
	week_start: number;
	date_format: 'dmy' | 'mdy' | 'ymd';
}

const defaults: UserPrefs = {
	timezone: 'UTC',
	time_format: '12h',
	week_start: 1,
	date_format: 'dmy'
};

export const prefs = writable<UserPrefs>(defaults);

export function prefsFromUser(u: User): UserPrefs {
	return {
		timezone: u.timezone,
		time_format: u.time_format ?? '12h',
		week_start: u.week_start ?? 1,
		date_format: u.date_format ?? 'dmy'
	};
}

function fmtDatePart(date: Date, format: 'dmy' | 'mdy' | 'ymd', timeZone: string): string {
	const parts = Object.fromEntries(new Intl.DateTimeFormat('en-GB', {
		timeZone, day: '2-digit', month: '2-digit', year: 'numeric'
	}).formatToParts(date).map(part => [part.type, part.value]));
	const { day: d, month: m, year: y } = parts;
	if (format === 'mdy') return `${m}/${d}/${y}`;
	if (format === 'ymd') return `${y}-${m}-${d}`;
	return `${d}/${m}/${y}`;
}

export function fmtDateTime(iso: string, p: UserPrefs = get(prefs)): string {
	const date = new Date(iso);
	const datePart = fmtDatePart(date, p.date_format, p.timezone);
	const timePart = date.toLocaleTimeString(undefined, {
		timeZone: p.timezone,
		hour: '2-digit',
		minute: '2-digit',
		hour12: p.time_format === '12h'
	});
	return `${datePart}, ${timePart}`;
}

export function fmtDate(ymd: string, p: UserPrefs = get(prefs)): string {
	// ymd is always YYYY-MM-DD from the API — parse directly to avoid TZ shift from new Date()
	const [y, m, d] = ymd.split('-');
	if (p.date_format === 'mdy') return `${m}/${d}/${y}`;
	if (p.date_format === 'ymd') return `${y}-${m}-${d}`;
	return `${d}/${m}/${y}`;
}

export function fmtTime(iso: string, p: UserPrefs = get(prefs)): string {
	return new Date(iso).toLocaleTimeString(undefined, {
		timeZone: p.timezone,
		hour: '2-digit',
		minute: '2-digit',
		hour12: p.time_format === '12h'
	});
}

export const WEEK_DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

export const TIMEZONES = [
	'Pacific/Auckland',
	'Australia/Sydney',
	'Australia/Melbourne',
	'Asia/Tokyo',
	'Asia/Singapore',
	'Asia/Dubai',
	'Asia/Jerusalem',
	'Europe/London',
	'Europe/Paris',
	'Europe/Berlin',
	'Europe/Amsterdam',
	'America/New_York',
	'America/Chicago',
	'America/Denver',
	'America/Los_Angeles',
	'UTC'
];
