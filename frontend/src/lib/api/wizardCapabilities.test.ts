import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ServersClient } from './clientServers';
import type { WizardKind, CapabilitiesResponse } from '$lib/types/serverWizard';

describe('ServersClient getWizardCapabilities normalization (Phase 0)', () => {
	let client: ServersClient;

	beforeEach(() => {
		client = new ServersClient();
	});

	it('safely populates empty arrays when backend returns minimal or empty data', async () => {
		vi.spyOn(client as any, 'request').mockResolvedValue({});

		const res = await client.getWizardCapabilities('xray');

		expect(res).toBeDefined();
		expect(res.kind).toBe('xray');
		expect(Array.isArray(res.profiles)).toBe(true);
		expect(res.profiles).toEqual([]);
		expect(Array.isArray(res.egress_options)).toBe(true);
		expect(res.egress_options).toEqual([]);
		expect(Array.isArray(res.modes)).toBe(true);
		expect(res.modes).toEqual([]);
		expect(Array.isArray(res.scenarios)).toBe(true);
		expect(res.scenarios).toEqual([]);
		expect(res.configured).toBe(false);
		expect(res.running).toBe(false);
		expect(res.recovery_required).toBe(false);
	});

	it('preserves complete capabilities when returned by server', async () => {
		const mockResponse: CapabilitiesResponse = {
			kind: 'tgwebproxy',
			scenarios: ['dual', 'direct_fake_tls'],
			modes: ['get', 'websocket'],
			profiles: [
				{
					id: 'cdn_get',
					name: 'CDN GET',
					description: 'Test',
					capabilities: ['get_only'],
					instructions: 'Instructions',
					recommended: true
				}
			],
			egress_options: [
				{
					id: 'direct',
					name: 'Direct WAN',
					kind: 'direct',
					owner: 'system',
					available: true,
					supports_tcp: true,
					supports_udp: true,
					generation: 1
				}
			],
			configured: true,
			running: true,
			recovery_required: false
		};

		vi.spyOn(client as any, 'request').mockResolvedValue(mockResponse);

		const res = await client.getWizardCapabilities('tgwebproxy');

		expect(res.kind).toBe('tgwebproxy');
		expect(res.scenarios).toEqual(['dual', 'direct_fake_tls']);
		expect(res.modes).toEqual(['get', 'websocket']);
		expect(res.profiles.length).toBe(1);
		expect(res.profiles[0].id).toBe('cdn_get');
		expect(res.egress_options.length).toBe(1);
		expect(res.egress_options[0].id).toBe('direct');
		expect(res.configured).toBe(true);
		expect(res.running).toBe(true);
		expect(res.recovery_required).toBe(false);
	});

	it('normalizes undefined or null fields without throwing exceptions', async () => {
		const partialResponse = {
			kind: 'xray',
			profiles: null,
			egress_options: undefined,
			modes: null,
			scenarios: undefined,
			configured: 1,
			running: 'true',
			recovery_required: 0
		};

		vi.spyOn(client as any, 'request').mockResolvedValue(partialResponse);

		const res = await client.getWizardCapabilities('xray');

		expect(res.kind).toBe('xray');
		expect(res.profiles).toEqual([]);
		expect(res.egress_options).toEqual([]);
		expect(res.modes).toEqual([]);
		expect(res.scenarios).toEqual([]);
		expect(res.configured).toBe(true);
		expect(res.running).toBe(true);
		expect(res.recovery_required).toBe(false);
	});

	it('propagates network rejections and API errors cleanly for UI error handling', async () => {
		vi.spyOn(client as any, 'request').mockRejectedValue(
			new Error('Network connection failed (502 Bad Gateway)')
		);

		await expect(client.getWizardCapabilities('xray')).rejects.toThrow(
			'Network connection failed (502 Bad Gateway)'
		);
	});

	it('handles completely null or primitive response values without crashing', async () => {
		vi.spyOn(client as any, 'request').mockResolvedValue(null);

		const res = await client.getWizardCapabilities('tgwebproxy');

		expect(res).toBeDefined();
		expect(res.kind).toBe('tgwebproxy');
		expect(res.profiles).toEqual([]);
		expect(res.egress_options).toEqual([]);
		expect(res.modes).toEqual([]);
		expect(res.scenarios).toEqual([]);
		expect(res.configured).toBe(false);
	});

	it('successfully recovers after a failed attempt on subsequent retry', async () => {
		const requestSpy = vi
			.spyOn(client as any, 'request')
			.mockRejectedValueOnce(new Error('Temporary router timeout'))
			.mockResolvedValueOnce({
				kind: 'xray',
				profiles: [{ id: 'cdn_get', name: 'CDN GET' }],
				egress_options: [{ id: 'direct', name: 'Direct' }]
			});

		// 1st attempt: fails
		await expect(client.getWizardCapabilities('xray')).rejects.toThrow('Temporary router timeout');

		// 2nd attempt (retry): succeeds
		const recovered = await client.getWizardCapabilities('xray');
		expect(recovered).toBeDefined();
		expect(recovered.profiles.length).toBe(1);
		expect(recovered.profiles[0].id).toBe('cdn_get');
		expect(recovered.egress_options.length).toBe(1);
		expect(recovered.egress_options[0].id).toBe('direct');
	});
});
