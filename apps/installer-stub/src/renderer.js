const button = document.querySelector("#check-updates");
const updateState = document.querySelector("#update-state");

button?.addEventListener("click", () => {
  if (updateState) {
    updateState.textContent = "Auto-update: auto-update wiring lands in feat/installer-pipeline R2";
  }
});
