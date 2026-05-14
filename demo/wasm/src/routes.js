import { slugNames, slugToStyle, styleToSlug } from './formats.js';

const defaultText = {
  title: 'llm2llm - Convert between LLM API formats online',
  heading: 'Convert any LLM API format to any other',
  intro: 'Paste an OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, or Google GenAI JSON payload and convert it to another LLM provider format in your browser.',
};

export function stylesFromLocation() {
  const parts = window.location.pathname.split('/').filter(Boolean);
  if (parts.length < 2) return null;

  const fromSlug = parts.at(-2);
  const toSlug = parts.at(-1);
  if (!slugToStyle[fromSlug] || !slugToStyle[toSlug] || fromSlug === toSlug) return null;
  return { fromStyle: slugToStyle[fromSlug], toStyle: slugToStyle[toSlug] };
}

export function routeText(fromStyle, toStyle) {
  const fromSlug = styleToSlug[fromStyle];
  const toSlug = styleToSlug[toStyle];
  if (!fromSlug || !toSlug || fromSlug === toSlug) return defaultText;

  const fromName = slugNames[fromSlug];
  const toName = slugNames[toSlug];
  return {
    title: `Convert ${fromName} to ${toName} JSON online - llm2llm`,
    heading: `Convert ${fromName} to ${toName} JSON`,
    intro: `Instantly convert ${fromName} API format to ${toName} format online. Supports request bodies, response bodies, and streaming chunks.`,
  };
}

export function conversionLinks() {
  return Object.keys(slugToStyle).flatMap((fromSlug) =>
    Object.keys(slugToStyle)
      .filter((toSlug) => toSlug !== fromSlug)
      .map((toSlug) => ({
        href: `/${fromSlug}/${toSlug}`,
        label: `${slugNames[fromSlug]} to ${slugNames[toSlug]}`,
      })),
  );
}

export function pushRoute(fromStyle, toStyle) {
  const fromSlug = styleToSlug[fromStyle];
  const toSlug = styleToSlug[toStyle];
  const path = fromSlug && toSlug && fromSlug !== toSlug ? `/${fromSlug}/${toSlug}` : '/';

  history.pushState(null, '', path);
  return routeText(fromStyle, toStyle);
}
