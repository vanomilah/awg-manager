import { describe, it, expect } from 'vitest';
import { cpuBarWidth, getCpuClass } from './shared';

describe('getCpuClass', () => {
	// Процесс: pct — доля всего процессора, пороги в ядрах.
	// Замер стенда: `yes`, упёршийся в ядро, — 23.6 % на 4 ядрах.
	it('процесс на 4 ядрах: 90 % ядра (22.5 %) — high, половина — med', () => {
		expect(getCpuClass(23.6, 4)).toBe('high');
		expect(getCpuClass(22.5, 4)).toBe('high');
		expect(getCpuClass(22.3, 4)).toBe('med');
		expect(getCpuClass(12.5, 4)).toBe('med');
		expect(getCpuClass(12.3, 4)).toBe('low');
	});

	it('процесс на 2 ядрах: пороги 45 % и 25 %', () => {
		expect(getCpuClass(45, 2)).toBe('high');
		expect(getCpuClass(44.8, 2)).toBe('med');
		expect(getCpuClass(25, 2)).toBe('med');
		expect(getCpuClass(24, 2)).toBe('low');
	});

	// 100/3 × 0.9 в float чуть больше 30, половина ядра приходит усечённой.
	it('процесс на 3 ядрах: 30 % — high, усечённая половина — med', () => {
		expect(getCpuClass(30, 3)).toBe('high');
		expect(getCpuClass(29.8, 3)).toBe('med');
		expect(getCpuClass(16.6, 3)).toBe('med');
		expect(getCpuClass(16.4, 3)).toBe('low');
	});

	// Полосы дашборда: pct — доля мощности, пороги прежние.
	it('без числа ядер — пороги 45 и 80', () => {
		expect(getCpuClass(80)).toBe('high');
		expect(getCpuClass(45)).toBe('med');
		expect(getCpuClass(44)).toBe('low');
	});
});

describe('cpuBarWidth', () => {
	it('полная полоса — одно ядро целиком', () => {
		expect(cpuBarWidth(25, 4)).toBe(100);
		expect(cpuBarWidth(12.5, 4)).toBe(50);
		expect(cpuBarWidth(60, 4)).toBe(100);
	});

	it('без числа ядер — доля как есть', () => {
		expect(cpuBarWidth(30)).toBe(30);
	});
});
