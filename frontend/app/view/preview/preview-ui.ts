// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { t, type Locale } from "@/app/i18n";
import { EntryManagerType } from "./entry-manager";

export function previewLabel(key: string, locale: Locale, params?: Record<string, string | number>): string {
    return t(key, params, locale);
}

export function getEntryManagerTypeLabel(type: EntryManagerType, locale: Locale): string {
    return t(type, undefined, locale);
}

export function formatPreviewLoadError(detail: string | undefined, locale: Locale): string {
    return t("Load Error: {detail}", { detail: detail ?? "" }, locale);
}

export function formatPreviewConnectionError(detail: string, locale: Locale): string {
    return t("Connection Error: {detail}", { detail }, locale);
}

export function formatPreviewMimetypeError(path: string, locale: Locale): string {
    return t("Unable to determine mimetype for: {path}", { path }, locale);
}

export function formatPreviewFileNotFound(fileName: string | undefined, locale: Locale): string {
    const suffix = fileName ? " " + JSON.stringify(fileName) : "";
    return t("File Not Found{suffix}", { suffix }, locale);
}

export function formatPreviewMimeTypePreview(mimeType: string, locale: Locale): string {
    return t("Preview ({mimeType})", { mimeType }, locale);
}

export function makeBookmarkMenuLabel(bookmarkLabel: string, path: string, locale: Locale): string {
    const localizedLabel = t(bookmarkLabel, undefined, locale);
    return t("Go to {label} ({path})", { label: localizedLabel, path }, locale);
}

export function makeDefaultFontSizeLabel(defaultFontSize: number, locale: Locale): string {
    return t("Default ({value})", { value: `${defaultFontSize}px` }, locale);
}