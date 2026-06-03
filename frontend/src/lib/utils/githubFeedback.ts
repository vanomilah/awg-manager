export const GITHUB_NEW_ISSUE_URL = 'https://github.com/hoaxisr/awg-manager/issues/new';

export function buildGitHubIssueUrl(title: string, body: string): string {
	const url = new URL(GITHUB_NEW_ISSUE_URL);
	url.searchParams.set('title', title);
	url.searchParams.set('body', body);
	return url.toString();
}

export function buildSuggestionIssueUrl(): string {
	const body = [
		'## Что хотите сообщить',
		'',
		'<!-- Опишите идею, предложение или вопрос -->',
		'',
		'## Важно',
		'',
		'AWG Manager — open-source проект без службы поддержки и SLA. Ответ не гарантируется.',
	].join('\n');
	return buildGitHubIssueUrl('Сообщение или предложение AWG Manager', body);
}
