(() => {
  "use strict";

  async function copyText(value) {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value);
      return;
    }
    const input = document.createElement("textarea");
    input.value = value;
    input.setAttribute("readonly", "");
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.append(input);
    input.select();
    const copied = document.execCommand("copy");
    input.remove();
    if (!copied) throw new Error("Clipboard unavailable");
  }

  document.addEventListener("click", async (event) => {
    const button = event.target.closest(".copy-button");
    if (!button) return;
    const value = button.dataset.copyValue;
    if (!value) return;
    const label = button.getAttribute("aria-label");
    try {
      await copyText(value);
      button.classList.add("copied");
      button.setAttribute("aria-label", "Copied");
      window.setTimeout(() => {
        button.classList.remove("copied");
        if (label) button.setAttribute("aria-label", label);
      }, 1600);
    } catch (error) {
      console.warn("Could not copy to clipboard", error);
    }
  });
})();
