// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { enUS } from "./en-US";
import type { I18nCatalog, I18nParams, Locale, LocaleOption } from "./types";
import { zhCN } from "./zh-CN";

const DefaultLocale: Locale = "en-US";

const localeCatalogs: Record<Locale, I18nCatalog> = {
    "en-US": enUS,
    "zh-CN": zhCN,
};

export const supportedLocales: LocaleOption[] = [
    { locale: "en-US", label: "English" },
    { locale: "zh-CN", label: "简体中文" },
];

const missingKeys = new Set<string>();

export function getMissingTranslationKeys(): string[] {
    return Array.from(missingKeys).sort();
}

export function clearMissingTranslationKeys(): void {
    missingKeys.clear();
}

function recordMissingKey(key: string, locale: Locale): void {
    if (locale === DefaultLocale) {
        return;
    }
    missingKeys.add(key);
}

function interpolate(template: string, params?: I18nParams): string {
    if (params == null) {
        return template;
    }
    return template.replace(/\{([a-zA-Z0-9_]+)\}/g, (match, key) => {
        const value = params[key];
        if (value == null) {
            return match;
        }
        return String(value);
    });
}

export function normalizeLocale(locale?: string | null): Locale | null {
    if (locale == null || locale === "") {
        return null;
    }
    const normalizedLocale = locale.replace("_", "-").toLowerCase();
    if (normalizedLocale === "en" || normalizedLocale === "en-us") {
        return "en-US";
    }
    if (normalizedLocale === "zh" || normalizedLocale === "zh-cn" || normalizedLocale.startsWith("zh-hans")) {
        return "zh-CN";
    }
    return null;
}

export function resolveLocale(configuredLocale?: string | null): Locale {
    return normalizeLocale(configuredLocale) ?? DefaultLocale;
}

export function t(key: string | null | undefined, params?: I18nParams, locale: Locale = DefaultLocale): string {
    if (key == null || key === "") {
        return "";
    }
    const catalog = localeCatalogs[locale] ?? localeCatalogs[DefaultLocale];
    const translated = catalog[key];
    if (translated == null && locale !== DefaultLocale) {
        recordMissingKey(key, locale);
    }
    return interpolate(translated ?? enUS[key] ?? key, params);
}

export type { I18nCatalog, I18nParams, Locale, LocaleOption };