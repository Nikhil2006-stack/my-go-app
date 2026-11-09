(() => {
  const root = document.documentElement;
  const themeToggle = document.getElementById("themeToggle");
  const themeIcon = document.getElementById("themeIcon");
  const savedTheme = localStorage.getItem("portfolio-theme");
  root.dataset.theme = savedTheme === "light" ? "light" : "dark";

  function updateThemeControl() {
    const isLight = root.dataset.theme === "light";
    themeIcon.textContent = isLight ? "☀" : "☾";
    themeToggle.setAttribute("aria-label", isLight ? "Switch to dark theme" : "Switch to light theme");
  }
  updateThemeControl();

  themeToggle.addEventListener("click", () => {
    root.dataset.theme = root.dataset.theme === "light" ? "dark" : "light";
    localStorage.setItem("portfolio-theme", root.dataset.theme);
    updateThemeControl();
  });

  const menuToggle = document.getElementById("menuToggle");
  const navLinks = document.getElementById("navLinks");
  menuToggle.addEventListener("click", () => {
    const open = navLinks.classList.toggle("open");
    menuToggle.setAttribute("aria-expanded", String(open));
    menuToggle.textContent = open ? "×" : "☰";
  });
  navLinks.querySelectorAll("a").forEach(link => link.addEventListener("click", () => {
    navLinks.classList.remove("open");
    menuToggle.setAttribute("aria-expanded", "false");
    menuToggle.textContent = "☰";
  }));
  document.getElementById("year").textContent = new Date().getFullYear();

  // Contact form frontend validation and async submission
  const contactForm = document.getElementById("contactForm");
  const contactAlert = document.getElementById("contactAlert");
  const submitBtn = document.getElementById("contactSubmitBtn");

  function showAlert(isSuccess, text) {
    if (!contactAlert) return;
    contactAlert.hidden = false;
    contactAlert.className = "form-alert " + (isSuccess ? "form-alert-success" : "form-alert-error");
    contactAlert.textContent = text;
  }

  if (contactForm) {
    contactForm.addEventListener("submit", async (e) => {
      e.preventDefault();

      const name = contactForm.elements["name"].value.trim();
      const email = contactForm.elements["email"].value.trim();
      const subject = contactForm.elements["subject"].value.trim();
      const message = contactForm.elements["message"].value.trim();

      if (name.length < 2 || name.length > 120) {
        showAlert(false, "Please enter your name (2–120 characters).");
        return;
      }
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
        showAlert(false, "Please enter a valid email address.");
        return;
      }
      if (subject.length < 2 || subject.length > 200) {
        showAlert(false, "Please enter a subject (2–200 characters).");
        return;
      }
      if (message.length < 10 || message.length > 4000) {
        showAlert(false, "Please enter a message of at least 10 characters.");
        return;
      }

      if (submitBtn) {
        submitBtn.disabled = true;
        submitBtn.textContent = "Sending...";
      }

      try {
        const formData = new URLSearchParams(new FormData(contactForm));
        const res = await fetch(contactForm.action, {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
            "Accept": "application/json"
          },
          body: formData.toString()
        });

        const payload = await res.json();
        if (res.ok && payload.ok) {
          showAlert(true, payload.message || "Thank you! Your message has been sent.");
          contactForm.reset();
        } else {
          showAlert(false, payload.message || "Could not send message. Please check your inputs.");
        }
      } catch (err) {
        showAlert(false, "Network error while sending message. Please try again.");
      } finally {
        if (submitBtn) {
          submitBtn.disabled = false;
          submitBtn.innerHTML = 'Send Message <span aria-hidden="true">↗</span>';
        }
      }
    });
  }
})();
