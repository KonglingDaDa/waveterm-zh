// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { resolveLocale, t, type I18nParams, type Locale } from "./index";

let cachedMainLocale: Locale = "zh-CN";

export function getLocaleFromFullConfig(fullConfig?: FullConfigType | null): Locale {
    return resolveLocale(fullConfig?.settings?.["app:locale"]);
}

export function setMainProcessLocale(locale: Locale): void {
    cachedMainLocale = locale;
}

export function getMainProcessLocale(): Locale {
    return cachedMainLocale;
}

export function updateMainProcessLocaleFromFullConfig(fullConfig?: FullConfigType | null): Locale {
    const locale = getLocaleFromFullConfig(fullConfig);
    cachedMainLocale = locale;
    return locale;
}

export function tMain(
    fullConfigOrLocale: FullConfigType | Locale | null | undefined,
    key: string | null | undefined,
    params?: I18nParams
): string {
    const locale =
        typeof fullConfigOrLocale === "string"
            ? resolveLocale(fullConfigOrLocale)
            : fullConfigOrLocale == null
              ? getMainProcessLocale()
              : getLocaleFromFullConfig(fullConfigOrLocale);
    return t(key, params, locale);
}