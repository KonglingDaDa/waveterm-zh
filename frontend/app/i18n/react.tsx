// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { atoms } from "@/app/store/global-atoms";
import { useAtomValue } from "jotai";
import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";
import { t, type I18nParams, type Locale } from "./index";

const LocaleContext = createContext<Locale>("en-US");

export function LocaleProvider({ children }: { children: ReactNode }) {
    const locale = useAtomValue(atoms.localeAtom);
    return <LocaleContext.Provider value={locale}>{children}</LocaleContext.Provider>;
}

export function useLocale(): Locale {
    return useContext(LocaleContext);
}

export function useT(): (key: string | null | undefined, params?: I18nParams) => string {
    const locale = useLocale();
    return useCallback((key, params) => t(key, params, locale), [locale]);
}

export function useLocaleAtom(): Locale {
    return useAtomValue(atoms.localeAtom);
}