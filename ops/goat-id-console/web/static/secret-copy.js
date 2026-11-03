// Copy only the credential the operator can already see; never log its value.
(function () {
  document.querySelectorAll('.secret').forEach(function (secret) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'secret-copy';
    button.textContent = 'Copy';
    var result = document.createElement('span');
    result.setAttribute('role', 'status');
    button.addEventListener('click', async function () {
      try {
        await navigator.clipboard.writeText(secret.textContent);
        result.textContent = ' Copied.';
      } catch (error) {
        result.textContent = ' Copy failed. Select the value and copy it manually.';
      }
    });
    secret.after(button, result);
  });
})();
