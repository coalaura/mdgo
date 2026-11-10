(() => {
	const notification = document.getElementById("notification"),
		csrf = document.querySelector(`input[name="csrf"]`)?.value ?? "";

	let busy = false,
		notificationTimer;

	function notify(message, failed = false) {
		clearTimeout(notificationTimer);

		notification.textContent = message;
		notification.classList.toggle("error", failed);
		notification.setAttribute("role", failed ? "alert" : "status");
		notification.hidden = false;

		notificationTimer = setTimeout(() => {
			notification.hidden = true;
		}, failed ? 10000 : 5000);
	}

	function setBusy(value) {
		busy = value;

		document.body.classList.toggle("busy", value);
		document.querySelector("main").setAttribute("aria-busy", String(value));

		for (const button of document.querySelectorAll("form[data-async] button")) {
			button.disabled = value || (button.hasAttribute("data-page-clear") && button.dataset.cached !== "true");
		}
	}

	function element(tag, className = "", text = "") {
		const node = document.createElement(tag);

		node.className = className;
		node.textContent = text;

		return node;
	}

	function hiddenField(name, value) {
		const input = document.createElement("input");

		input.type = "hidden";
		input.name = name;
		input.value = value;

		return input;
	}

	function pageRow(page) {
		const row = document.createElement("tr");

		row.dataset.pagePath = page.path;

		const name = document.createElement("td"),
			link = element("a", "page-link", page.path);

		link.href = page.url;

		name.append(link);

		const status = document.createElement("td");

		status.append(element("span", page.cached ? "status cached" : "status", page.cached ? "Cached" : "Not cached"));

		const actions = element("td", "actions"),
			form = document.createElement("form");

		form.method = "post";
		form.action = "/-/cache/page";
		form.dataset.async = "";

		const button = element("button", "quiet", "Clear cache");

		button.type = "submit";
		button.dataset.pageClear = "";
		button.dataset.cached = String(page.cached);
		button.setAttribute("aria-label", `Clear cache for ${page.path}`);
		button.disabled = busy || !page.cached;

		form.append(hiddenField("csrf", csrf), hiddenField("path", page.path), button);
		actions.append(form);
		row.append(name, status, actions);

		return row;
	}

	function updateDashboard(dashboard) {
		const pages = dashboard.pages ?? [],
			rows = document.getElementById("page-rows"),
			existing = new Map();

		for (const row of rows.children) {
			existing.set(row.dataset.pagePath, row);
		}

		let cursor = rows.firstElementChild;

		for (const page of pages) {
			const row = existing.get(page.path) ?? pageRow(page);

			existing.delete(page.path);

			const status = row.querySelector(".status");

			status.textContent = page.cached ? "Cached" : "Not cached";
			status.classList.toggle("cached", page.cached);

			row.querySelector("[data-page-clear]").dataset.cached = String(page.cached);

			if (row === cursor) {
				cursor = cursor.nextElementSibling;
			} else {
				rows.insertBefore(row, cursor);
			}
		}

		for (const row of existing.values()) {
			row.remove();
		}

		document.getElementById("page-count").textContent = String(pages.length);
		document.getElementById("cached-count").textContent = String(dashboard.cached);
		document.getElementById("uncached-count").textContent = String(dashboard.uncached);
		document.getElementById("page-table").hidden = pages.length === 0;
		document.getElementById("empty-state").hidden = pages.length !== 0;
	}

	document.addEventListener("submit", async (event) => {
		const form = event.target;

		if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-async")) {
			return;
		}

		event.preventDefault();

		if (busy) {
			return;
		}

		const body = new URLSearchParams(new FormData(form)),
			options = {
				method: form.method.toUpperCase(),
				credentials: "same-origin",
				headers: { Accept: "application/json" }
			};

		if (options.method !== "GET") {
			options.body = body;
		}

		setBusy(true);

		try {
			const response = await fetch(form.action, options),
				result = await response.json();

			if (result.redirect) {
				window.location.assign(result.redirect);

				return;
			}

			if (!response.ok) {
				notify(result.error || "The request failed. Please try again.", true);

				return;
			}

			if (result.dashboard) {
				updateDashboard(result.dashboard);
			}

			notify(result.message || "Status refreshed.");
		} catch {
			notify("The request could not be completed. Refresh to check its status.", true);
		} finally {
			setBusy(false);
		}
	});
})();
