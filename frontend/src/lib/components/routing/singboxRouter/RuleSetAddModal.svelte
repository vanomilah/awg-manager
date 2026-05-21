<script lang="ts">
	import Modal from '$lib/components/ui/Modal.svelte';
	import { Button, Dropdown, SyntaxHighlightedTextarea, type DropdownOption } from '$lib/components/ui';
	import { highlightJson } from '$lib/utils/shareEditorHighlight';
	import { highlightInlineRuleListContent } from '$lib/utils/singboxInlineRulesHighlight';
	import { api } from '$lib/api/client';
	import type { GeoFileEntry, SingboxRouterRuleSet } from '$lib/types';
	import type { OutboundGroup } from './outboundOptions';
	import { HrNeoGeoTagPicker } from '$lib/components/hrneo';
	import {
		analyzeInlineRuleListLossy,
		isInlineRuleListEmpty,
		parseInlineRuleList,
		stringifyInlineRuleList,
		validateRuleSetTag,
	} from '$lib/utils/singboxInlineRules';
	import { expandGeoLinesInInput } from '$lib/utils/singboxInlineGeoExpand';

	interface Props {
		ruleSet?: SingboxRouterRuleSet;
		outboundOptions: OutboundGroup[];
		onClose: () => void;
		onSave: (rs: SingboxRouterRuleSet) => Promise<void> | void;
	}
	let { ruleSet, outboundOptions, onClose, onSave }: Props = $props();

	// ── constants ───────────────────────────────────────────────
	const UPDATE_INTERVAL_OPTIONS: DropdownOption[] = [
		{ value: '6h', label: '6h' },
		{ value: '12h', label: '12h' },
		{ value: '24h', label: '24h (рекомендуется)' },
		{ value: '168h', label: '168h (неделя)' },
	];

	const RULES_LIST_PLACEHOLDER = `# Домены и все поддомены
chatgpt.com
*.openai.com 
https://gemini.google.com/app

# Только поддомены
.perplexity.ai
domain_suffix:deepseek.com

# Только домен
domain:claude.ai

# IP/CIDR
1.1.1.1
8.8.8.0/24

# Дополнительно
keyword:youtube
geosite:xai`;

	// ── derived ────────────────────────────────────────────────
	const downloadDetourOptions = $derived<DropdownOption[]>([
		{ value: '', label: 'автоматически (direct)' },
		...outboundOptions.flatMap((g) =>
			g.items.map((i) => ({ value: i.value, label: i.label, group: g.group })),
		),
	]);

	const isEditing = $derived(Boolean(ruleSet));

	let geoFiles = $state<GeoFileEntry[]>([]);
	let geositePickerOpen = $state(false);
	let geoipPickerOpen = $state(false);
	let expandedRulesList = $state('');
	let geoExpandWarnings = $state<string[]>([]);
	let geoExpanding = $state(false);

	let geositeFiles = $derived(geoFiles.filter((g) => g.type === 'geosite').map((g) => g.path));
	let geoipFiles = $derived(geoFiles.filter((g) => g.type === 'geoip').map((g) => g.path));

	$effect(() => {
		void (async () => {
			try {
				geoFiles = (await api.getGeoFiles()) ?? [];
			} catch {
				geoFiles = [];
			}
		})();
	});

	$effect(() => {
		if (type !== 'inline' || inlineMode !== 'list') {
			expandedRulesList = '';
			geoExpandWarnings = [];
			return;
		}
		const input = rulesList;
		const timer = setTimeout(() => {
			void (async () => {
				geoExpanding = true;
				try {
					const { text, warnings } = await expandGeoLinesInInput(input, async (kind, tag) => {
						const res = await api.expandGeoTag(kind, tag);
						return res.lines;
					});
					if (input === rulesList) {
						expandedRulesList = text;
						geoExpandWarnings = warnings;
					}
				} catch {
					if (input === rulesList) {
						expandedRulesList = input;
						geoExpandWarnings = [];
					}
				} finally {
					if (input === rulesList) geoExpanding = false;
				}
			})();
		}, 350);
		return () => clearTimeout(timer);
	});

	// ── inline preview derived ──────────────────────────────────
	const listParsePreview = $derived.by(() => {
		if (type !== 'inline' || inlineMode !== 'list') {
			return { rules: [] as Record<string, unknown>[], warnings: [] as string[], errors: [] as string[] };
		}
		const source = expandedRulesList || rulesList;
		const parsed = parseInlineRuleList(source);
		if (geoExpandWarnings.length === 0) return parsed;
		return {
			...parsed,
			warnings: [...geoExpandWarnings, ...parsed.warnings],
		};
	});

	function appendRulesLine(token: string): void {
		const trimmed = rulesList.trimEnd();
		rulesList = trimmed ? `${trimmed}\n${token}` : token;
	}

	function appendGeositeLine(token: string): void {
		appendRulesLine(token);
		geositePickerOpen = false;
	}

	// ── line numbers for list / JSON editors ─────────────────────
	function lineNumbersFor(text: string): string {
		const count = Math.max(1, text.split(/\r?\n/).length);
		return Array.from({ length: count }, (_, i) => String(i + 1)).join('\n');
	}

	let rulesListTextarea = $state<HTMLTextAreaElement | null>(null);
	let rulesListLineNumberGutter = $state<HTMLPreElement | null>(null);
	let rulesJsonTextarea = $state<HTMLTextAreaElement | null>(null);
	let rulesJsonLineNumberGutter = $state<HTMLPreElement | null>(null);

	function syncLineNumberGutter(
		ta: HTMLTextAreaElement | null,
		gutter: HTMLPreElement | null,
	): void {
		if (!ta || !gutter) return;
		gutter.scrollTop = ta.scrollTop;
	}

	function syncRulesListLineNumbersScroll(): void {
		syncLineNumberGutter(rulesListTextarea, rulesListLineNumberGutter);
	}

	function syncRulesJsonLineNumbersScroll(): void {
		syncLineNumberGutter(rulesJsonTextarea, rulesJsonLineNumberGutter);
	}

	// ── form state ──────────────────────────────────────────────
	// For inline rule sets: 'list' = smart-line-by-line, 'json' = raw JSON array
	let inlineMode: 'list' | 'json' = $state('list');

	let rulesList = $state('');

	const listInputEmpty = $derived(isInlineRuleListEmpty(rulesList));

	let type: 'remote' | 'local' | 'inline' = $state('remote');
	let format: 'binary' | 'source' = $state('binary');
	let tag = $state('');
	let url = $state('');
	let updateInterval = $state('24h');
	let downloadDetour = $state('');
	let path = $state('');
	let rulesJson = $state('');

	const rulesListLineNumbers = $derived(lineNumbersFor(rulesList));
	const rulesJsonLineNumbers = $derived(lineNumbersFor(rulesJson));

	let busy = $state(false);
	let error = $state('');
	let inlineModeBusy = $state(false);
	/** Geo/list-parse or JSON→list serializer messages shown after switching Список ↔ JSON */
	let inlineTabConvertWarnings = $state<string[]>([]);

	async function switchInlineMode(next: 'list' | 'json'): Promise<void> {
		if (next === inlineMode || inlineModeBusy) return;
		error = '';
		inlineTabConvertWarnings = [];
		if (type !== 'inline') {
			inlineMode = next;
			return;
		}
		inlineModeBusy = true;
		try {
			if (next === 'json' && inlineMode === 'list') {
				if (isInlineRuleListEmpty(rulesList)) {
					rulesJson = DEFAULT_RULES_JSON;
					inlineMode = 'json';
					return;
				}

				const { text: expanded, warnings: geoWarn } = await expandGeoLinesInInput(
					rulesList,
					async (kind, tag) => {
						const res = await api.expandGeoTag(kind, tag);
						return res.lines;
					},
				);
				const parsed = parseInlineRuleList(expanded);
				if (parsed.errors.length > 0) {
					error = parsed.errors.join('\n');
					return;
				}
				rulesJson =
					parsed.rules.length === 0 ? '[]' : JSON.stringify(parsed.rules, null, 2);
				inlineTabConvertWarnings = [...geoWarn, ...parsed.warnings];
				inlineMode = 'json';
				return;
			}
			if (next === 'list' && inlineMode === 'json') {
				const trimmed = rulesJson.trim();
				if (trimmed === '') {
					rulesList = '';
					inlineMode = 'list';
					return;
				}
				let arr: unknown;
				try {
					arr = JSON.parse(rulesJson);
				} catch (e) {
					error = `Некорректный JSON: ${(e as Error).message}`;
					return;
				}
				if (!Array.isArray(arr)) {
					error = 'Правила должны быть JSON-массивом';
					return;
				}
				const typed = arr as Record<string, unknown>[];
				rulesList = stringifyInlineRuleList(typed);
				const lossy = analyzeInlineRuleListLossy(typed);
				inlineTabConvertWarnings = lossy.issues;
				inlineMode = 'list';
				return;
			}
			inlineMode = next;
		} finally {
			inlineModeBusy = false;
		}
	}

	$effect(() => {
		if (type !== 'inline') inlineTabConvertWarnings = [];
	});

	const inlineLossyAnalysis = $derived.by(() => {
		if (!ruleSet || ruleSet.type !== 'inline') return { lossy: false, issues: [] as string[] };
		return analyzeInlineRuleListLossy(ruleSet.rules);
	});

	// Default rulesJson template for new rule sets (must match $state initializer above)
	const DEFAULT_RULES_JSON = `[
  {
    "domain_suffix": [
      "example.com"
    ]
  }
]`;

	// Snapshot initial state for isDirty detection
	let initialInlineMode: 'list' | 'json' = $state('list');
	let initialType: 'remote' | 'local' | 'inline' = $state('remote');
	let initialFormat: 'binary' | 'source' = $state('binary');
	let initialTag = $state('');
	let initialUrl = $state('');
	let initialUpdateInterval = $state('24h');
	let initialDownloadDetour = $state('');
	let initialPath = $state('');
	let initialRulesList = $state('');
	let initialRulesJson = $state('');

	const formResetKey = $derived(`${isEditing ? 'edit' : 'create'}:${ruleSet?.tag ?? ''}`);

	$effect(() => {
		void formResetKey;

		const nextType: 'remote' | 'local' | 'inline' = ruleSet?.type ?? 'remote';
		const nextFormat: 'binary' | 'source' = ruleSet?.format ?? 'binary';
		const nextTag = ruleSet?.tag ?? '';
		const nextUrl = ruleSet?.url ?? '';
		const nextUpdateInterval = ruleSet?.update_interval ?? '24h';
		const nextDownloadDetour = ruleSet?.download_detour ?? '';
		const nextPath = ruleSet?.path ?? '';
		const nextRulesJson = ruleSet?.rules?.length
			? JSON.stringify(ruleSet.rules, null, 2)
			: DEFAULT_RULES_JSON;
		const nextLossyAnalysis =
			ruleSet?.type === 'inline'
				? analyzeInlineRuleListLossy(ruleSet.rules)
				: { lossy: false, issues: [] as string[] };
		const nextInlineMode: 'list' | 'json' =
			ruleSet?.type === 'inline' && ruleSet?.rules?.length && nextLossyAnalysis.lossy
				? 'json'
				: 'list';
		const nextRulesList =
			nextType === 'inline' && ruleSet?.rules?.length
				? stringifyInlineRuleList(ruleSet.rules)
				: '';

		type = nextType;
		format = nextFormat;
		tag = nextTag;
		url = nextUrl;
		updateInterval = nextUpdateInterval;
		downloadDetour = nextDownloadDetour;
		path = nextPath;
		rulesJson = nextRulesJson;
		inlineMode = nextInlineMode;
		rulesList = nextRulesList;

		initialType = nextType;
		initialFormat = nextFormat;
		initialTag = nextTag;
		initialUrl = nextUrl;
		initialUpdateInterval = nextUpdateInterval;
		initialDownloadDetour = nextDownloadDetour;
		initialPath = nextPath;
		initialRulesJson = nextRulesJson;
		initialInlineMode = nextInlineMode;
		initialRulesList = nextRulesList;

		error = '';
		busy = false;
		inlineTabConvertWarnings = [];
	});

	const isDirty = $derived.by(() => {
		return (
			type !== initialType ||
			format !== initialFormat ||
			tag !== initialTag ||
			url !== initialUrl ||
			updateInterval !== initialUpdateInterval ||
			downloadDetour !== initialDownloadDetour ||
			path !== initialPath ||
			inlineMode !== initialInlineMode ||
			rulesList !== initialRulesList ||
			rulesJson !== initialRulesJson
		);
	});

	async function save(): Promise<void> {
		busy = true;
		error = '';
		try {
			const cleanTag = isEditing ? (ruleSet?.tag ?? '') : tag.trim();
			const tagErr = validateRuleSetTag(cleanTag);
			if (tagErr) {
				error = tagErr;
				busy = false;
				return;
			}
			if (type === 'remote' && !url.trim()) {
				error = 'URL обязателен для type=remote';
				busy = false;
				return;
			}
			if (type === 'local' && !path.trim()) {
				error = 'Path обязателен для type=local';
				busy = false;
				return;
			}

			let parsedRules: Record<string, unknown>[] | undefined;
			if (type === 'inline') {
				if (isEditing && inlineMode === 'list' && inlineLossyAnalysis.lossy) {
					error = 'Этот JSON содержит поля, которые режим Список не может сохранить без потерь. Редактируйте в JSON.';
					busy = false;
					return;
				}
				if (inlineMode === 'json') {
					try {
						const parsed = JSON.parse(rulesJson);
						if (!Array.isArray(parsed) || parsed.length === 0) {
							error = 'Для inline rule set нужен непустой JSON-массив правил';
							busy = false;
							return;
						}
						parsedRules = parsed as Record<string, unknown>[];
					} catch (e) {
						error = `Некорректный JSON: ${(e as Error).message}`;
						busy = false;
						return;
					}
				} else {
					if (isInlineRuleListEmpty(rulesList)) {
						error = 'Список пуст';
						busy = false;
						return;
					}
					const { text: expanded, warnings: geoWarn } = await expandGeoLinesInInput(
						rulesList,
						async (kind, tag) => (await api.expandGeoTag(kind, tag)).lines,
					);
					const parsed = parseInlineRuleList(expanded);
					if (parsed.errors.length > 0) {
						error = parsed.errors.join('\n');
						busy = false;
						return;
					}
					if (parsed.rules.length === 0) {
						error = 'Нет валидных строк для inline rule set';
						busy = false;
						return;
					}
					parsedRules = parsed.rules;
				}
			}

			const built: SingboxRouterRuleSet = {
				tag: cleanTag,
				type,
				format: type === 'inline' ? undefined : format,
				url: type === 'remote' ? url.trim() : undefined,
				update_interval: type === 'remote' ? updateInterval : undefined,
				download_detour: type === 'remote' && downloadDetour ? downloadDetour : undefined,
				path: type === 'local' ? path.trim() : undefined,
				rules: type === 'inline' ? parsedRules : undefined,
			};
			await onSave(built);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<Modal open onclose={onClose} title={ruleSet ? 'Редактировать rule set' : 'Новый rule set'} hasUnsavedChanges={() => isDirty}>
	<div class="form">
		<div class="field">
			<div class="lbl">Тип</div>
			<div class="segment">
				<button class:active={type === 'remote'} onclick={() => (type = 'remote')} type="button">Remote</button>
				<button class:active={type === 'local'} onclick={() => (type = 'local')} type="button">Local</button>
				<button class:active={type === 'inline'} onclick={() => (type = 'inline')} type="button">Inline</button>
			</div>
		</div>

		<label class="field">
			<div class="lbl">Tag (имя)</div>
			<input bind:value={tag} placeholder="geosite-example" disabled={isEditing} />
			{#if isEditing}
				<div class="hint">Tag нельзя менять у существующего набора.</div>
			{:else}
				<div class="hint">Не используйте суффикс -srs — он добавляется автоматически для скомпилированного набора.</div>
			{/if}
		</label>

		{#if type !== 'inline'}
			<label class="field">
				<div class="lbl">Формат</div>
				<div class="segment">
					<button class:active={format === 'binary'} onclick={() => (format = 'binary')} type="button">Binary (.srs)</button>
					<button class:active={format === 'source'} onclick={() => (format = 'source')} type="button">Source (JSON)</button>
				</div>
			</label>
		{/if}

		{#if type === 'remote'}
			<label class="field">
				<div class="lbl">URL к файлу</div>
				<input bind:value={url} placeholder="https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-example.srs" />
			</label>

			<label class="field">
				<div class="lbl">Интервал обновления</div>
				<Dropdown bind:value={updateInterval} options={UPDATE_INTERVAL_OPTIONS} fullWidth />
			</label>

			<div class="field highlight">
				<div class="lbl">Скачивать через (download detour)</div>
				<Dropdown bind:value={downloadDetour} options={downloadDetourOptions} fullWidth />
				<div class="hint">
					Через какой outbound скачивать этот файл. Полезно если URL заблокирован у провайдера — используйте VPN-туннель.
				</div>
			</div>
		{:else if type === 'local'}
			<label class="field">
				<div class="lbl">Путь к файлу</div>
				<input bind:value={path} placeholder="/opt/etc/awg-manager/singbox/config.d/rule-sets/my.srs" />
				<div class="hint">Абсолютный путь. Файл должен существовать на роутере.</div>
			</label>
		{:else}
			<div class="field">
				<div class="lbl">Формат ввода</div>
				<div class="segment">
					<button
						class:active={inlineMode === 'list'}
						disabled={inlineModeBusy}
						onclick={() => void switchInlineMode('list')}
						type="button"
					>
						Список
					</button>
					<button
						class:active={inlineMode === 'json'}
						disabled={inlineModeBusy}
						onclick={() => void switchInlineMode('json')}
						type="button"
					>
						JSON
					</button>
				</div>
				{#if inlineTabConvertWarnings.length > 0}
					<div class="parse-messages parse-messages-warning">
						<div class="parse-messages-title">При переключении режима</div>
						<ul>
							{#each inlineTabConvertWarnings as msg}
								<li>{msg}</li>
							{/each}
						</ul>
					</div>
				{/if}
			</div>

			{#if inlineMode === 'list'}
				<div class="field">
					<div class="list-toolbar">
						<div class="lbl" class:lbl-expanding={geoExpanding}>
							{geoExpanding ? 'Разворачиваем geosite:/geoip: теги…' : 'Список правил'}
						</div>
						<div class="list-toolbar-actions">
							<Button variant="ghost" size="sm" onclick={() => (geositePickerOpen = !geositePickerOpen)}>
								+ geosite:TAG
							</Button>
							<Button variant="ghost" size="sm" onclick={() => (geoipPickerOpen = !geoipPickerOpen)}>
								+ geoip:TAG
							</Button>
						</div>
					</div>
					{#if geositePickerOpen}
						<HrNeoGeoTagPicker
							kind="geosite"
							files={geositeFiles}
							onpick={appendGeositeLine}
							onclose={() => (geositePickerOpen = false)}
						/>
					{/if}
					{#if geoipPickerOpen}
						<HrNeoGeoTagPicker
							kind="geoip"
							files={geoipFiles}
							onpick={(t) => appendRulesLine(t)}
							onclose={() => (geoipPickerOpen = false)}
						/>
					{/if}
					<div class="rules-editor">
						<pre class="line-numbers" aria-hidden="true" bind:this={rulesListLineNumberGutter}>{rulesListLineNumbers}</pre>
						<div class="rules-editor-input">
							<SyntaxHighlightedTextarea
								bind:value={rulesList}
								bind:textareaRef={rulesListTextarea}
								highlight={highlightInlineRuleListContent}
								wrap="pre"
								class="rules-list-ta"
								placeholder={RULES_LIST_PLACEHOLDER}
								onscroll={syncRulesListLineNumbersScroll}
							/>
						</div>
					</div>
				{#if isEditing && inlineLossyAnalysis.lossy}
					<div class="parse-messages parse-messages-warning">
						<div class="parse-messages-title">Потеря данных в режиме “Список”</div>
						<ul>
							<li>Этот JSON содержит поля, которые режим Список не может сохранить без потерь. Редактируйте в JSON.</li>
							{#each inlineLossyAnalysis.issues as issue}
								<li>{issue}</li>
							{/each}
						</ul>
					</div>
				{/if}
				<details class="inline-help">
					<summary>Подсказка по формату списка</summary>

					<div class="inline-help-body">
						<p class="inline-help-intro">
							Одна строка — одно значение. Пустые строки игнорируются. <br>
							В списке можно писать комментарии: <code>#</code>, <code>//</code>, <code>;</code> (целая строка или в конце — после пробела). <br>
							При сохранении или переходе на вкладку JSON комментарии удаляются и в JSON не сохраняются. Порядок строк в списке не имеет значения и может меняться при сохранении.
						</p>

						<section class="inline-help-section">
							<div class="help-label">Домены и поддомены</div>
							<ul>
								<li><code>domain.com</code>, <code>*.domain.com</code>, <code>domain_suffix:domain.com</code> → хост и его поддомены (в JSON: <code>"domain.com"</code> без точки)</li>
								<li><code>https://example.domain.com/…</code> — из URL берётся hostname и хранится так же</li>
								<li><code>*.рф</code> — доменная зона; кириллица будет конвертирована в punycode <code>xn--p1ai</code> без ведущей точки</li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">Только поддомены (без отдельного <code>domain</code>)</div>
							<ul>
								<li><code>.domain.com</code> — суффикс <em>с</em> точкой в JSON: <code>[".domain.com"]</code> (apex не матчится)</li>
								<li><code>domain_suffix:.domain.com</code> — явная dotted-форма, в JSON будет <code>".domain.com"</code></li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">Только точный хост</div>
							<ul>
								<li><code>domain:domain.com</code> — только <code>domain</code>, без поддоменов</li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">IP и подсети (только IPv4)</div>
							<ul>
								<li><code>1.1.1.1</code> — в JSON как <code>1.1.1.1/32</code>; при обратном переводе снова голый IP</li>
								<li><code>8.8.8.0/24</code> — CIDR как есть; маски кроме <code>/32</code> не сжимаются</li>
								<li>префиксы <code>ip:</code>, <code>cidr:</code>, <code>src_ip:</code> — то же правило</li>
								<li>IPv6 в режиме «Список» не поддерживается — адреса и префиксы IPv6 задавайте в JSON</li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">Geo и прочие matchers</div>
							<ul>
								<li><code>geosite:TAG</code> — разворачивается в домены; суффиксы из .dat — <strong>без</strong> ведущей точки (как <code>domain.com</code>)</li>
								<li><code>geoip:TAG</code> — разворачивается в CIDR; одиночные хосты из geo — в списке без <code>/32</code></li>
								<li><code>keyword:TAG</code>, <code>regex:…</code> — отдельные поля в JSON</li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">Расширенные matchers</div>
							<ul>
								<li><code>port:443</code>, <code>process:curl</code>, <code>package:…</code>, <code>network:tcp|udp</code></li>
								<li>каждый тип — отдельная группа правил в JSON (не смешивается с доменами в одной записи)</li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">Осторожно</div>
							<ul>
								<li><code>port:443</code> — отдельное правило на весь HTTPS-трафик</li>
								<li><code>process:</code> / <code>process_path:</code> на Keenetic/Entware — только локальные процессы роутера, не LAN-клиенты</li>
							</ul>
						</section>

						<section class="inline-help-section">
							<div class="help-label">Пока не в режиме «Список»</div>
							<ul>
								<li>IPv6, исключения <code>@@</code>, <code>port_range:</code></li>
								<li>логика <code>and</code> / <code>or</code> и любые лишние поля sing-box — только режим <code>JSON</code></li>
							</ul>
						</section>
					</div>
				</details>
				</div>

				{#if listInputEmpty}
					<div class="hint list-empty-hint">Список пуст — добавьте домены, IP или geosite:/geoip: тег.</div>
				{:else if listParsePreview.errors.length > 0}
					<div class="parse-messages parse-messages-error">
						<div class="parse-messages-title">Ошибки разбора</div>
						<ul>
							{#each listParsePreview.errors as msg}
								<li>{msg}</li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if listParsePreview.warnings.length > 0}
					<div class="parse-messages parse-messages-warning">
						<div class="parse-messages-title">Предупреждения</div>
						<ul>
							{#each listParsePreview.warnings as msg}
								<li>{msg}</li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if listParsePreview.rules.length > 0}
					<div class="info">
						Будет создано групп правил: {listParsePreview.rules.length}
					</div>
				{/if}
				{#if listParsePreview.rules.length > 0}
					<details class="json-preview">
						<summary>Предпросмотр JSON</summary>
						<pre>{JSON.stringify(listParsePreview.rules, null, 2)}</pre>
					</details>
				{/if}
			{:else}
				<label class="field">
					<div class="lbl">Правила (JSON-массив)</div>
					<div class="rules-editor rules-json-editor">
						<pre
							class="line-numbers"
							aria-hidden="true"
							bind:this={rulesJsonLineNumberGutter}
						>{rulesJsonLineNumbers}</pre>
						<div class="rules-editor-input">
							<SyntaxHighlightedTextarea
								bind:value={rulesJson}
								bind:textareaRef={rulesJsonTextarea}
								highlight={highlightJson}
								indentMode="json"
								wrap="pre-wrap"
								class="rules-json-ta"
								onscroll={syncRulesJsonLineNumbersScroll}
							/>
						</div>
					</div>
					<div class="hint">
						Advanced mode: массив объектов с матчерами sing-box.
					</div>
				</label>
			{/if}
		{/if}

		{#if error}<div class="error">{error}</div>{/if}
	</div>

	{#snippet actions()}
		<Button variant="ghost" size="md" onclick={onClose} type="button">Отмена</Button>
		<Button
			variant="primary"
			size="md"
			onclick={save}
			disabled={busy || inlineModeBusy}
			loading={busy || inlineModeBusy}
			type="button"
		>
			Сохранить
		</Button>
	{/snippet}
</Modal>

<style>
	.form {
		display: grid;
		gap: 0.6rem;
		min-width: 0;
	}
	.field {
		display: grid;
		gap: 0.25rem;
	}
	.field.highlight {
		padding: 0.6rem;
		background: var(--bg);
		border-left: 2px solid var(--accent, #3b82f6);
		border-radius: 4px;
	}
	.lbl {
		font-size: 0.75rem;
		color: var(--muted-text);
	}
	.lbl-expanding {
		color: var(--accent, #3b82f6);
	}
	.list-toolbar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		flex-wrap: wrap;
	}
	.list-toolbar-actions {
		display: flex;
		gap: 0.35rem;
		flex-wrap: wrap;
	}
	.hint {
		font-size: 0.75rem;
		color: var(--muted-text);
		line-height: 1.4;
		margin-top: 0.25rem;
	}
	.inline-help {
		margin-top: 0.35rem;
		padding: 0.5rem 0.65rem;
		border: 1px solid var(--border);
		border-radius: 0.45rem;
		background: var(--surface-1, rgba(255, 255, 255, 0.035));
		color: var(--muted-text);
		font-size: 0.8rem;
		line-height: 1.45;
	}

	.inline-help summary {
		cursor: pointer;
		color: var(--text);
		font-weight: 700;
		outline: none;
	}

	.inline-help-body {
		margin-top: 0.45rem;
		display: grid;
		gap: 0.55rem;
	}

	.inline-help-intro {
		margin: 0;
	}

	.inline-help-section ul {
		margin: 0.2rem 0 0;
		padding-left: 1.15rem;
	}

	.inline-help-section li {
		margin: 0.12rem 0;
	}

	.inline-help-section li::marker {
		color: var(--muted-text);
	}

	.inline-help code {
		background: var(--surface-2, rgba(255, 255, 255, 0.06));
		border-radius: 0.25rem;
		padding: 0.05rem 0.25rem;
		font-size: 0.78rem;
	}

	.help-label {
		color: var(--text);
		font-weight: 600;
	}
	.field input {
		background: var(--bg);
		border: 1px solid var(--border);
		padding: 0.4rem 0.6rem;
		border-radius: 4px;
		color: var(--text);
		font-family: ui-monospace, monospace;
		font-size: 0.85rem;
		width: 100%;
		box-sizing: border-box;
	}
	.field input:disabled {
		opacity: 0.7;
		cursor: not-allowed;
	}
	.rules-json-editor {
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: 4px;
		width: 100%;
		box-sizing: border-box;
		height: calc(10 * 1.45em + 1rem);
	}

	.rules-json-editor:focus-within {
		border-color: var(--accent, #3b82f6);
	}
	.segment {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 4px;
		overflow: hidden;
		width: fit-content;
	}
	.segment button {
		background: transparent;
		border: none;
		padding: 0.4rem 0.9rem;
		font-size: 0.85rem;
		cursor: pointer;
		color: var(--muted-text);
	}
	.segment button + button {
		border-left: 1px solid var(--border);
	}
	.segment button.active {
		background: var(--accent, #3b82f6);
		color: var(--color-accent-contrast, #ffffff);
		font-weight: 600;
	}
	.error {
		color: var(--danger, #dc2626);
		font-size: 0.85rem;
	}
	.parse-messages {
		padding: 0.55rem 0.65rem;
		border: 1px solid var(--border);
		border-radius: 0.45rem;
		background: var(--surface-1, rgba(255, 255, 255, 0.035));
		font-size: 0.82rem;
		line-height: 1.4;
		max-height: min(12rem, 32vh);
		overflow: auto;
		overflow-wrap: anywhere;
		word-break: break-word;
		min-width: 0;
	}

	.parse-messages-title {
		font-weight: 700;
		margin-bottom: 0.35rem;
	}

	.parse-messages ul {
		margin: 0;
		padding-left: 1.1rem;
		display: grid;
		gap: 0.22rem;
	}

	.parse-messages li {
		margin: 0;
	}

	.parse-messages-error {
		border-color: var(--danger, #dc2626);
		color: var(--danger, #dc2626);
		background: rgba(220, 38, 38, 0.08);
	}

	.parse-messages-warning {
		border-color: var(--color-warning, #d97706);
		color: var(--color-warning, #d97706);
		background: rgba(217, 119, 6, 0.08);
	}
	.info {
		color: #10b981;
		font-size: 0.85rem;
	}
	.json-preview {
		margin: 0;
	}
	.json-preview summary {
		cursor: pointer;
		font-size: 0.85rem;
		color: var(--accent, #3b82f6);
		outline: none;
	}
	.json-preview pre {
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: 4px;
		padding: 0.5rem 0.6rem;
		font-size: 0.75rem;
		overflow-x: auto;
		margin-top: 0.25rem;
	}
	.rules-editor {
		display: grid;
		grid-template-columns: auto 1fr;
		height: 16rem;
		min-height: 8rem;
		max-height: min(70vh, 36rem);
		resize: vertical;
		overflow: hidden;
		min-width: 0;
		align-items: stretch;
	}
	.line-numbers {
		margin: 0;
		padding: 0.5rem 0.45rem 0.5rem 0.55rem;
		border-right: 1px solid var(--border);
		background: var(--bg);
		color: var(--muted-text);
		font-family: ui-monospace, monospace;
		font-size: 0.8rem;
		line-height: 1.45;
		text-align: right;
		user-select: none;
		overflow: hidden;
		min-width: 2.25rem;
		white-space: pre;
		height: 100%;
		box-sizing: border-box;
	}
	.rules-editor-input {
		min-width: 0;
		min-height: 0;
		height: 100%;
		padding: 0.5rem 0.6rem;
		box-sizing: border-box;
	}

	.rules-editor-input :global(.shl-stack) {
		font-family: ui-monospace, monospace;
		font-size: 0.8rem;
		line-height: 1.45;
	}

	.rules-json-editor :global(.shl-stack) {
		font-family: ui-monospace, monospace;
		font-size: 0.8rem;
		line-height: 1.45;
	}

</style>
