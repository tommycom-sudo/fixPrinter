import './style.css';
import './app.css';

import {
	DefaultPrintParams,
	StartPrint,
	NotifyPrintResult,
	PausePrinter,
	ResumePrinter,
	GetPrinterJobs,
	GetPrinterStatus,
	RemovePrintJob,
} from '../wailsjs/go/main/App';

const state = {
	defaultPayload: null,
	isPrinting: false,
	jobs: [],
	jobsTimer: null,
	printerStatus: null,
	autoDeleteEnabled: false,
	deletedJobsCount: 0,
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
	if (dom.pauseButton) {
		dom.pauseButton.disabled = isBusy;
	}
	if (dom.resumeButton) {
		dom.resumeButton.disabled = isBusy;
	}
	dom.printButton.innerText = isBusy ? '执行中…' : '执行打印';
	dom.page.classList.toggle('page--busy', isBusy);
}

function setJobsStatus(message, isError = false) {
	if (!dom.jobsStatus) {
		return;
	}
	dom.jobsStatus.textContent = message;
	dom.jobsStatus.classList.toggle('jobs__status--error', isError);
}

function renderJobs() {
	if (!dom.jobsBody) {
		return;
	}
	const jobs = Array.isArray(state.jobs) ? state.jobs : [];
	dom.jobsBody.replaceChildren();

	if (jobs.length === 0) {
		if (dom.jobsTable) {
			dom.jobsTable.classList.add('jobs-table--hidden');
		}
		if (dom.jobsEmpty) {
			dom.jobsEmpty.classList.add('jobs__empty--visible');
		}
		return;
	}

	if (dom.jobsTable) {
		dom.jobsTable.classList.remove('jobs-table--hidden');
	}
	if (dom.jobsEmpty) {
		dom.jobsEmpty.classList.remove('jobs__empty--visible');
	}

	jobs.forEach((job) => {
		const row = document.createElement('tr');
		row.innerHTML = `
      <td>${job?.id ?? '-'}</td>
      <td>${job?.computerName || '—'}</td>
      <td>${job?.printerName || '—'}</td>
      <td>${job?.documentName || '暂无文件名'}</td>
      <td>${job?.submittedTime || '—'}</td>
      <td>${job?.jobStatus || '未知'}</td>
    `;
		dom.jobsBody.appendChild(row);
	});
}

async function checkPrinterStatus() {
	try {
		const status = await GetPrinterStatus('MS');
		state.printerStatus = status;
		return status;
	} catch (error) {
		console.error('检查打印机状态失败', error);
		return null;
	}
}

async function refreshJobs(showLoading = false) {
	if (showLoading) {
		setJobsStatus('正在提取 MS 打印任务…');
	}

	try {
		// 检查打印机状态
		const status = await checkPrinterStatus();
		const isPaused = status && status.isPaused;

		// 获取任务列表
		const jobs = await GetPrinterJobs('MS');
		const previousJobs = state.jobs;
		state.jobs = Array.isArray(jobs) ? jobs : [];

		// 如果打印机处于暂停状态，自动删除新任务
		if (isPaused && state.autoDeleteEnabled && state.jobs.length > 0) {
			// 找出新任务（之前不存在的任务）
			const previousJobIds = new Set(previousJobs.map(j => j.id));
			const newJobs = state.jobs.filter(job => !previousJobIds.has(job.id));

			// 删除所有新任务
			for (const job of newJobs) {
				try {
					await RemovePrintJob('MS', job.id);
					state.deletedJobsCount++;
					console.log(`[AutoDelete] 已删除任务 #${job.id}: ${job.documentName || '未知文档'}`);
				} catch (error) {
					console.error(`[AutoDelete] 删除任务 #${job.id} 失败:`, error);
				}
			}

			// 如果有新任务被删除，重新获取任务列表
			if (newJobs.length > 0) {
				const updatedJobs = await GetPrinterJobs('MS');
				state.jobs = Array.isArray(updatedJobs) ? updatedJobs : [];
			}
		}

		renderJobs();
		const count = state.jobs.length;
		
		// 更新状态显示
		if (isPaused && state.autoDeleteEnabled) {
			if (count === 0) {
				setJobsStatus(`MS 打印队列为空（已自动删除 ${state.deletedJobsCount} 个任务）`);
			} else {
				setJobsStatus(`MS 队列中有 ${count} 个任务（暂停中，已自动删除 ${state.deletedJobsCount} 个任务）`);
			}
		} else {
			if (count === 0) {
				setJobsStatus('MS 打印队列为空');
			} else {
				setJobsStatus(`MS 队列中有 ${count} 个任务`);
			}
		}
	} catch (error) {
		console.error('提取打印任务失败', error);
		const message = error && error.message ? error.message : '无法获取打印任务';
		setJobsStatus(message, true);
	}
}

function stopJobsMonitor() {
	if (state.jobsTimer) {
		clearInterval(state.jobsTimer);
		state.jobsTimer = null;
	}
}

function startJobsMonitor() {
	stopJobsMonitor();
	// 初始化时检查打印机状态，如果暂停则启用自动删除并清理现有任务
	checkPrinterStatus().then(async status => {
		if (status && status.isPaused) {
			state.autoDeleteEnabled = true;
			console.log('[AutoDelete] 检测到打印机已暂停，启用自动删除功能');
			// 清理暂停时已有的任务
			try {
				const jobs = await GetPrinterJobs('MS');
				if (Array.isArray(jobs) && jobs.length > 0) {
					for (const job of jobs) {
						try {
							await RemovePrintJob('MS', job.id);
							state.deletedJobsCount++;
							console.log(`[AutoDelete] 启动时删除现有任务 #${job.id}`);
						} catch (error) {
							console.error(`[AutoDelete] 删除任务 #${job.id} 失败:`, error);
						}
					}
				}
			} catch (error) {
				console.error('启动时获取任务列表失败:', error);
			}
		}
		refreshJobs(true);
	});
	state.jobsTimer = setInterval(refreshJobs, 5000);
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

async function handlePausePrinter() {
	setStatus('正在暂停打印机 MS …');
	try {
		await PausePrinter('MS');
		setStatus('打印机 MS 已暂停，正在清理队列中的任务…');
		// 启用自动删除
		state.autoDeleteEnabled = true;
		state.deletedJobsCount = 0;
		
		// 立即获取并删除所有现有任务
		try {
			const jobs = await GetPrinterJobs('MS');
			if (Array.isArray(jobs) && jobs.length > 0) {
				for (const job of jobs) {
					try {
						await RemovePrintJob('MS', job.id);
						state.deletedJobsCount++;
						console.log(`[AutoDelete] 已删除现有任务 #${job.id}: ${job.documentName || '未知文档'}`);
					} catch (error) {
						console.error(`[AutoDelete] 删除任务 #${job.id} 失败:`, error);
					}
				}
			}
		} catch (error) {
			console.error('获取任务列表失败:', error);
		}
		
		// 刷新任务列表
		await refreshJobs(false);
		setStatus(`打印机 MS 已暂停，已清理 ${state.deletedJobsCount} 个任务，将自动删除新任务。`);
	} catch (error) {
		const message = error && error.message ? error.message : '暂停打印机失败';
		setStatus(message, true);
	}
}

async function handleResumePrinter() {
	setStatus('正在恢复打印机 MS …');
	try {
		await ResumePrinter('MS');
		setStatus('打印机 MS 已恢复。');
		// 禁用自动删除
		state.autoDeleteEnabled = false;
		// 刷新状态显示
		await refreshJobs(false);
	} catch (error) {
		const message = error && error.message ? error.message : '恢复打印机失败';
		setStatus(message, true);
	}
}

function bindEvents() {
	dom.printButton.addEventListener('click', handlePrint);
	dom.resetButton.addEventListener('click', loadDefaults);
	if (dom.pauseButton) {
		dom.pauseButton.addEventListener('click', handlePausePrinter);
	}
	if (dom.resumeButton) {
		dom.resumeButton.addEventListener('click', handleResumePrinter);
	}
	if (dom.refreshJobsButton) {
		dom.refreshJobsButton.addEventListener('click', () => refreshJobs(true));
	}
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
              <button id="pause-btn" class="ghost ghost--warn">暂停打印机</button>
              <button id="resume-btn" class="ghost ghost--success">恢复打印机</button>
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
        <section class="panel panel--jobs">
          <div class="panel__header">
            <h2>打印任务监控</h2>
            <div class="panel__actions">
              <button id="refresh-jobs-btn" class="ghost">手动刷新</button>
            </div>
          </div>
          <div class="jobs__status" id="jobs-status">等待获取 MS 打印队列…</div>
          <div class="jobs__table-wrapper">
            <table class="jobs-table jobs-table--hidden" id="jobs-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>主机</th>
                  <th>打印机</th>
                  <th>文档</th>
                  <th>提交时间</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody id="jobs-body"></tbody>
            </table>
            <div class="jobs__empty" id="jobs-empty">当前打印队列为空</div>
          </div>
          <p class="jobs__hint">
            每 5 秒调用 <code>Get-PrintJob -PrinterName "MS"</code> 获取任务列表，便于实时监控。
          </p>
        </section>
      </div>
    </div>
  `;

	dom.page = document.getElementById('page');
	dom.editor = document.getElementById('payload-editor');
	dom.printButton = document.getElementById('print-btn');
	dom.resetButton = document.getElementById('reset-btn');
	dom.pauseButton = document.getElementById('pause-btn');
	dom.resumeButton = document.getElementById('resume-btn');
	dom.status = document.getElementById('status-text');
	dom.previewFrame = document.getElementById('report-frame');
	dom.jobsTable = document.getElementById('jobs-table');
	dom.jobsBody = document.getElementById('jobs-body');
	dom.jobsStatus = document.getElementById('jobs-status');
	dom.jobsEmpty = document.getElementById('jobs-empty');
	dom.refreshJobsButton = document.getElementById('refresh-jobs-btn');
}

async function bootstrap() {
	mountUI();
	bindEvents();

	window.__xAutoPrint = {
		start: executePrint,
	};

	await loadDefaults();
	window.addEventListener('beforeunload', stopJobsMonitor);
	startJobsMonitor();
}

bootstrap();
