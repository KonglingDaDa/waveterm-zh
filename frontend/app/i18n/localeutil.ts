// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { atoms } from "@/app/store/global-atoms";
import { globalStore } from "@/app/store/jotaiStore";
import { t, type I18nParams, type Locale } from "./index";

export function getCurrentLocale(): Locale {
    return globalStore.get(atoms.localeAtom);
}

export function tCurrent(key: string | null | undefined, params?: I18nParams): string {
    return t(key, params, getCurrentLocale());
}