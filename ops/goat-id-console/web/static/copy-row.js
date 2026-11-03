(function () {
  document.addEventListener("click", async function (event) {
    if (!(event.target instanceof Element)) return;
    var button = event.target.closest("[data-copy-button]");
    if (!button) return;
    var row = button.closest("[data-copy-row]");
    var source = document.getElementById(button.dataset.copySource || "");
    var status = row && row.querySelector("[data-copy-status]");
    if (!source || !status) return;
    try {
      await navigator.clipboard.writeText(source.value);
      status.textContent = "Copied";
    } catch (_) {
      status.textContent = "Copy failed. Select the value and copy it manually.";
    }
  });
})();
