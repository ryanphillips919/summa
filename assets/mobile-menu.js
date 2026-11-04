(() => {
  const menu = document.querySelector("#mobile-menu");
  const toggle = document.querySelector("#mobile-menu-toggle");
  const closeButton = document.querySelector(".mobile-menu-close");

  window.lucide?.createIcons();
  if (!menu || !toggle || !closeButton) return;

  const closeMenu = () => {
    if (menu.open) menu.close();
    toggle.setAttribute("aria-expanded", "false");
    toggle.focus();
  };

  toggle.addEventListener("click", () => {
    if (menu.open) {
      closeMenu();
      return;
    }
    menu.showModal();
    document.documentElement.classList.add("mobile-menu-open");
    toggle.setAttribute("aria-expanded", "true");
    closeButton.focus();
  });

  closeButton.addEventListener("click", closeMenu);
  menu.querySelectorAll("a").forEach((link) => link.addEventListener("click", () => {
    if (menu.open) menu.close();
  }));
  menu.addEventListener("close", () => {
    toggle.setAttribute("aria-expanded", "false");
    document.documentElement.classList.remove("mobile-menu-open")
  });
  menu.addEventListener("click", (event) => {
    if (event.target === menu) closeMenu();
  });
})();
