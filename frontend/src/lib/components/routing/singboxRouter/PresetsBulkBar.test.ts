import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import PresetsBulkBar from './PresetsBulkBar.svelte';

describe('PresetsBulkBar', () => {
	it('renders nothing when selectedCount is 0', () => {
		const { container } = render(PresetsBulkBar, {
			props: { selectedCount: 0, preflightStatus: 'ok', onApply: vi.fn(), onClear: vi.fn() },
		});
		expect(container.textContent?.trim()).toBe('');
	});

	it('renders count + Apply enabled when preflight ok', () => {
		const onApply = vi.fn();
		render(PresetsBulkBar, {
			props: { selectedCount: 3, preflightStatus: 'ok', onApply, onClear: vi.fn() },
		});
		expect(screen.getByText(/3/)).toBeTruthy();
		const applyBtn = screen.getByRole('button', { name: /применить/i });
		expect(applyBtn.hasAttribute('disabled')).toBe(false);
		fireEvent.click(applyBtn);
		expect(onApply).toHaveBeenCalledOnce();
	});

	it('disables Apply when preflight not ok', () => {
		render(PresetsBulkBar, {
			props: { selectedCount: 2, preflightStatus: 'no-policy', onApply: vi.fn(), onClear: vi.fn() },
		});
		const applyBtn = screen.getByRole('button', { name: /применить/i });
		expect(applyBtn.hasAttribute('disabled')).toBe(true);
	});

	it('calls onClear on cancel click', () => {
		const onClear = vi.fn();
		render(PresetsBulkBar, {
			props: { selectedCount: 1, preflightStatus: 'ok', onApply: vi.fn(), onClear },
		});
		const clearBtn = screen.getByRole('button', { name: /отменить выбор|✕|сбросить/i });
		fireEvent.click(clearBtn);
		expect(onClear).toHaveBeenCalledOnce();
	});
});
