const form = document.querySelector("[data-journal-form]");

if (form) {
  const lines = form.querySelector("[data-journal-lines]");
  const template = document.querySelector("#journal-line-template");
  const debitTotal = form.querySelector("[data-debit-total]");
  const creditTotal = form.querySelector("[data-credit-total]");
  const state = form.querySelector("[data-balance-state]");
  const post = form.querySelector("[data-post-adjustment]");
  const amount = (value) => /^\d+(\.\d{1,2})?$/.test(value.trim()) ? Math.round(Number(value) * 100) : 0;
  const money = (cents) => new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" }).format(cents / 100);
  const updateTotals = () => {
    const debits = [...form.querySelectorAll("[data-debit]")].reduce((sum, input) => sum + amount(input.value), 0);
    const credits = [...form.querySelectorAll("[data-credit]")].reduce((sum, input) => sum + amount(input.value), 0);
    const balanced = debits > 0 && debits === credits;
    debitTotal.textContent = money(debits); creditTotal.textContent = money(credits);
    state.textContent = balanced ? "Balanced and ready to post" : "Enter balanced amounts to post";
    state.classList.toggle("balanced", balanced); post.disabled = !balanced;
  };
  form.querySelector("[data-add-line]").addEventListener("click", () => {
    lines.insertAdjacentHTML("beforeend", template.innerHTML);
    if (window.lucide) window.lucide.createIcons(); updateTotals();
  });
  lines.addEventListener("input", updateTotals);
  lines.addEventListener("click", (event) => {
    const remove = event.target.closest("[data-remove-line]");
    if (remove && lines.querySelectorAll(".journal-line").length > 2) { remove.closest(".journal-line").remove(); updateTotals(); }
  });
  updateTotals();
}
