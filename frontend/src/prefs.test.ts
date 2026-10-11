import { describe, expect, it } from 'vitest';
import { fmtDateTime, fmtTime, type UserPrefs } from './lib/prefs';

const tokyo: UserPrefs = { timezone: 'Asia/Tokyo', time_format: '24h', week_start: 1, date_format: 'dmy' };

describe('host time-zone formatting', () => {
	it('uses the host calendar date when the same instant crosses midnight', () => {
		expect(fmtDateTime('2026-01-01T23:30:00Z', tokyo)).toBe('02/01/2026, 08:30');
	});
	it('uses the host time zone independently of the browser time zone', () => {
		expect(fmtTime('2026-01-01T23:30:00Z', tokyo)).toBe('08:30');
		expect(fmtTime('2026-01-01T23:30:00Z', { ...tokyo, timezone: 'America/Los_Angeles' })).toBe('15:30');
	});
});
