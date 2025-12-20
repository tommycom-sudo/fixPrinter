import './style.css';
import './app.css';

import {
	DefaultPrintParams,
	StartPrint,
	NotifyPrintResult,
} from '../wailsjs/go/main/App';

const state = {
	defaultPayload: null,
	isPrinting: false,
};

const dom = {};

function setStatus(message, isError = false) {
	if (!dom.status) {
		return;
	}
	dom.status.textContent = message;
	dom.status.classList.toggle('status--error', isError);
}

function setBusy(isBusy) {
	state.isPrinting = isBusy;
	if (!dom.printButton) {
		return;
	}
	dom.printButton.disabled = isBusy;
	dom.resetButton.disabled = isBusy;
	dom.printButton.innerText = isBusy ? '执行中…' : '执行打印';
	dom.page.classList.toggle('page--busy', isBusy);
}

async function loadDefaults() {
	try {
		const defaults = await DefaultPrintParams();
		state.defaultPayload = defaults;
		dom.editor.value = JSON.stringify(defaults, null, 2);
		setStatus('已载入默认参数，可根据需要调整后再打印。');
	} catch (error) {
		console.error(error);
		setStatus(`无法获取默认参数：${error.message}`, true);
	}
}

function parsePayload() {
	const raw = dom.editor.value.trim();
	if (raw.length === 0) {
		throw new Error('打印参数不能为空');
	}
	try {
		return JSON.parse(raw);
	} catch (error) {
		throw new Error(`打印参数 JSON 无效：${error.message}`);
	}
}

function buildCacheBustingURL(url) {
	const sep = url.includes('?') ? '&' : '?';
	return `${url}${sep}_=${Date.now()}`;
}

function loadReportFrame(entryUrl, timeout) {
	return new Promise((resolve, reject) => {
		if (!entryUrl) {
			reject(new Error('未配置 entryUrl，无法打开 FineReport 页面'));
			return;
		}
		const target = buildCacheBustingURL(entryUrl);

		let settled = false;
		const iframe = dom.previewFrame;

		const cleanup = () => {
			iframe.removeEventListener('load', onLoad);
			clearTimeout(timer);
		};

		const onLoad = () => {
			if (settled) {
				return;
			}
			cleanup();
			settled = true;
			resolve(iframe);
		};

		const timer = setTimeout(() => {
			if (settled) {
				return;
			}
			cleanup();
			settled = true;
			reject(new Error(`FineReport 页面加载超时（${timeout}ms）`));
		}, timeout || 20000);

		iframe.addEventListener('load', onLoad, { once: true });
		iframe.src = target;
	});
}

function waitForFR(frameWindow, timeoutMs, intervalMs) {
	return new Promise((resolve, reject) => {
		const started = performance.now();
		const interval = intervalMs || 300;

		const watcher = setInterval(() => {
			if (!frameWindow || frameWindow.closed) {
				clearInterval(watcher);
				reject(new Error('FineReport 窗口不可用'));
				return;
			}

			if (frameWindow.FR && typeof frameWindow.FR.doURLPrint === 'function') {
				clearInterval(watcher);
				resolve(frameWindow.FR);
				return;
			}

			if (performance.now() - started > timeoutMs) {
				clearInterval(watcher);
				reject(new Error('等待 FineReport 对象超时'));
			}
		}, interval);
	});
}

async function executePrint(payload) {
	const started = performance.now();
	const result = {
		requestId: payload.requestId,
		success: false,
	};

	try {
		const frame = await loadReportFrame(payload.entryUrl, payload.frameLoadTimeoutMs);
		const win = frame.contentWindow;
		await waitForFR(win, payload.readyTimeoutMs, payload.readyIntervalMs);
		win.FR.doURLPrint(payload);
		result.success = true;
	} catch (error) {
		console.error('[AutoPrint] 执行失败', error);
		result.error = error.message;
	} finally {
		result.durationMs = Math.round(performance.now() - started);
		try {
			await NotifyPrintResult(result);
		} catch (notifyErr) {
			console.error('回传打印结果失败', notifyErr);
		}
	}
}

async function handlePrint() {
	let payload;
	try {
		payload = parsePayload();
	} catch (error) {
		setStatus(error.message, true);
		return;
	}

	setBusy(true);
	setStatus('正在打开 FineReport 页面并注入打印参数…');

	try {
		const result = await StartPrint(payload);
		const duration =
			result && result.durationMs >= 0 ? `${result.durationMs} ms` : '未知';
		setStatus(`打印完成（耗时 ${duration}）。`);
	} catch (error) {
		const message = error && error.message ? error.message : '打印失败';
		setStatus(message, true);
	} finally {
		setBusy(false);
	}
}

function bindEvents() {
	dom.printButton.addEventListener('click', handlePrint);
	dom.resetButton.addEventListener('click', loadDefaults);
}

function mountUI() {
	dom.app = document.querySelector('#app');
	dom.app.innerHTML = `
    <div class="page" id="page">
      <header class="hero">
        <div>
          <p class="hero__eyebrow">FineReport 自动化打印</p>
          <h1 class="hero__title">药方快速打印工具</h1>
          <p class="hero__subtitle">
            自动打开指定报表、等待 FR 对象就绪，并调用 <code>FR.doURLPrint</code> 完成打印。
          </p>
        </div>
        <div class="hero__status" id="status-text">正在初始化…</div>
      </header>
      <div class="workarea">
        <section class="panel panel--editor">
          <div class="panel__header">
            <h2>打印参数</h2>
            <div class="panel__actions">
              <button id="reset-btn" class="ghost">恢复默认</button>
              <button id="print-btn">执行打印</button>
            </div>
          </div>
          <textarea id="payload-editor" spellcheck="false"></textarea>
        </section>
        <section class="panel panel--preview">
          <div class="panel__header">
            <h2>FineReport 会话</h2>
          </div>
          <div class="preview__body">
            <iframe id="report-frame" title="FineReport session"></iframe>
            <p class="preview__hint">
              该视图仅用于加载远端报表并执行打印命令，不会在界面中暴露处方数据。
            </p>
          </div>
        </section>
      </div>
    </div>
  `;

	dom.page = document.getElementById('page');
	dom.editor = document.getElementById('payload-editor');
	dom.printButton = document.getElementById('print-btn');
	dom.resetButton = document.getElementById('reset-btn');
	dom.status = document.getElementById('status-text');
	dom.previewFrame = document.getElementById('report-frame');
}

async function bootstrap() {
	mountUI();
	bindEvents();

	window.__xAutoPrint = {
		start: executePrint,
	};

	await loadDefaults();
}

bootstrap();
