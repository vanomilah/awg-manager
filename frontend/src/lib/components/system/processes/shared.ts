export type SortField = 'cpu' | 'mem' | 'pid' | 'name' | 'user' | 'threads' | 'state' | 'time';

export type CpuLevel = 'low' | 'med' | 'high';

// Бэкенд отбрасывает всё после десятых (33.33… приходит как 33.3), а
// 100/3 × 0.9 в float — 30.000000000000004: без допуска граница не берётся.
const TRUNC_TOLERANCE = 0.1;

// Процесс, упёршийся в ядро, целого ядра не получает — часть уходит на
// прерывания и соседей: `yes` на MT7621 (4 ядра) — 23.6–23.8 % вместо 25 %
// (стенд 29.09, F544). Поэтому high — от 90 % ядра.
const SATURATED_CORE = 0.9;

// Без cpuCount pct — доля мощности ядра или всего процессора (полосы
// дашборда). С cpuCount pct — доля всего процессора, занятая процессом, и
// пороги считаются в ядрах: ½ ядра — med, 90 % ядра — high (однопоточный
// процесс выше одного ядра не поднимется).
export function getCpuClass(pct: number, cpuCount = 0): CpuLevel {
	if (cpuCount > 0) {
		const core = 100 / cpuCount;
		if (pct >= core * SATURATED_CORE - TRUNC_TOLERANCE) return 'high';
		if (pct >= core / 2 - TRUNC_TOLERANCE) return 'med';
		return 'low';
	}
	if (pct >= 80) return 'high';
	if (pct >= 45) return 'med';
	return 'low';
}

// Ширина мини-полосы процесса в той же шкале, что и цвет: полная полоса —
// одно ядро целиком. Без cpuCount — доля всего процессора, как есть.
export function cpuBarWidth(pct: number, cpuCount = 0): number {
	return Math.min(100, cpuCount > 0 ? pct * cpuCount : pct);
}
