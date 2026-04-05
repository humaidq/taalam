/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */

document.addEventListener("DOMContentLoaded", () => {
  const viewer = document.querySelector("[data-lesson-viewer]");
  if (!viewer) {
    return;
  }

  const slides = Array.from(viewer.querySelectorAll("[data-lesson-slide]"));
  const prevButton = viewer.querySelector("[data-lesson-prev]");
  const nextButton = viewer.querySelector("[data-lesson-next]");
  const positionLabel = viewer.querySelector("[data-lesson-position]");
  const completeForm = viewer.querySelector("[data-lesson-complete]");

  if (slides.length === 0 || !prevButton || !nextButton || !positionLabel) {
    return;
  }

  let index = 0;

  const render = () => {
    slides.forEach((slide, slideIndex) => {
      slide.classList.toggle("lesson-slide-active", slideIndex === index);
      slide.setAttribute("aria-hidden", slideIndex === index ? "false" : "true");
    });

    positionLabel.textContent = String(index + 1);
    prevButton.disabled = index === 0;

    const isLastSlide = index === slides.length - 1;
    nextButton.disabled = isLastSlide;

    if (completeForm) {
      completeForm.hidden = !isLastSlide;
      nextButton.hidden = isLastSlide;
    }
  };

  prevButton.addEventListener("click", () => {
    if (index > 0) {
      index -= 1;
      render();
    }
  });

  nextButton.addEventListener("click", () => {
    if (index < slides.length - 1) {
      index += 1;
      render();
    }
  });

  document.addEventListener("keydown", (event) => {
    const target = event.target;
    if (target instanceof HTMLElement) {
      const tagName = target.tagName.toLowerCase();
      if (tagName === "textarea" || tagName === "input" || target.isContentEditable) {
        return;
      }
    }

    if (event.key === "ArrowLeft" && index > 0) {
      index -= 1;
      render();
    }
    if (event.key === "ArrowRight" && index < slides.length - 1) {
      index += 1;
      render();
    }
  });

  render();
});
