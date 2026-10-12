// Полоса бинарей (PG-04..PG-10) — данные обоих продуктов одной формой.
// Наличие бинарей берётся из install-статуса подсистемы, а НЕ из инстанса:
// инстансов может не быть вовсе, а бинари при этом лежат на диске.

import type { FreeTurnStatus, OpenFluxStatus, WdttStatus } from '$lib/types';

export type ProxyBinaryKind = 'wdtt' | 'freeturn' | 'openflux';

export function binaryStripItems(
	wdtt: WdttStatus | null,
	ft: FreeTurnStatus | null,
	openflux: OpenFluxStatus | null,
	installing: ProxyBinaryKind | null,
	oninstall: (kind: ProxyBinaryKind) => void,
) {
	return [
		{
			name: 'wdtt',
			binaryPresent: wdtt?.binariesPresent === true,
			installAvailable: wdtt?.installAvailable === true,
			installing: installing === 'wdtt' || wdtt?.installing === true,
			updateAvailable: wdtt?.updateAvailable === true,
			installedVersion: wdtt?.installedVersion,
			installVersion: wdtt?.installVersion,
			oninstall: () => oninstall('wdtt'),
		},
		{
			name: 'freeturn',
			binaryPresent: ft?.binariesPresent === true,
			installAvailable: ft?.installAvailable === true,
			installing: installing === 'freeturn' || ft?.installing === true,
			updateAvailable: ft?.updateAvailable === true,
			installedVersion: ft?.installedVersion,
			installVersion: ft?.installVersion,
			oninstall: () => oninstall('freeturn'),
		},
		{
			name: 'openflux',
			binaryPresent: openflux?.installed === true,
			installAvailable: openflux?.archSupported !== false,
			installing: installing === 'openflux',
			updateAvailable: Boolean(
				openflux?.latestVersion && openflux?.version && openflux.latestVersion !== openflux.version,
			),
			installedVersion: openflux?.version,
			installVersion:
				openflux?.latestVersion && openflux?.version !== openflux.latestVersion
					? openflux.latestVersion
					: undefined,
			oninstall: () => oninstall('openflux'),
		},
	];
}
