import type en from './en';

const cs: Record<keyof typeof en, string> = {
	'app.name': 'Trayline',

	'nav.projects': 'Projekty',
	'nav.logout': 'Odhlásit',
	'nav.menu': 'Menu',

	'auth.title': 'Trayline Dashboard',
	'auth.subtitle': 'Pro připojení zadejte API token.',
	'auth.placeholder': 'Zadejte API token...',
	'auth.connect': 'Připojit',
	'auth.error': 'S tímto tokenem se nepodařilo připojit. Zkontrolujte ho a zkuste to znovu.',

	'projects.empty': 'Nebyly nalezeny žádné synchronizované projekty.',
	'projects.error': 'Projekty se nepodařilo načíst.',
	'projects.branch': 'Větev',
	'projects.uncommittedChanges': 'Neuložené změny',

	'project.error': 'Tento projekt se nepodařilo načíst.',

	'tabs.files': 'Soubory',
	'tabs.commits': 'Commity',
	'tabs.changes': 'Změny',
	'tabs.env': 'Prostředí',

	'commits.empty': 'Nebyly nalezeny žádné commity.',
	'commits.error': 'Commity se nepodařilo načíst.',
	'commits.loadMore': 'Načíst další',
	'commits.detail.back': 'Zpět na commity',
	'commits.detail.error': 'Tento commit se nepodařilo načíst.',
	'commits.detail.files': '{count} souborů',

	'diff.tooLarge': 'Diff je příliš velký na zobrazení',

	'files.emptyDir': 'Tento adresář je prázdný.',
	'files.emptyFile': 'Prázdný soubor',
	'files.notFound': 'Soubor nebo adresář nebyl nalezen.',
	'files.error': 'Tuto cestu se nepodařilo načíst.',
	'files.binary': 'Binární soubor ({size}) — nelze zobrazit',
	'files.truncated': 'Soubor je příliš velký na zobrazení ({size})',
	'files.raw': 'Raw',
	'files.root': 'Kořen',

	'env.key': 'Klíč',
	'env.value': 'Hodnota',
	'env.keyPlaceholder': 'NAZEV_PROMENNE',
	'env.addVariable': 'Přidat proměnnou',
	'env.save': 'Uložit',
	'env.delete': 'Smazat proměnnou',
	'env.confirmDelete': 'Smazat tuto proměnnou?',
	'env.reveal': 'Zobrazit hodnotu',
	'env.hide': 'Skrýt hodnotu',
	'env.error.empty_key': 'Klíč je povinný',
	'env.error.invalid_key': 'Neplatný název proměnné',
	'env.error.duplicate': 'Duplicitní klíč',

	'error.connection.title': 'Server je nedostupný',
	'error.connection.message':
		'Dashboard se nepodařilo připojit k serveru Trayline. Zkontrolujte připojení a zkuste to znovu.',
	'error.connection.retry': 'Zkusit znovu',

	'error.fallback.title': 'Něco se pokazilo',
	'error.fallback.message': 'Došlo k neočekávané chybě. Zkuste to prosím znovu.',
	'error.fallback.retry': 'Načíst znovu',

	'common.loading': 'Načítání...',
	'common.retry': 'Zkusit znovu'
};

export default cs;
