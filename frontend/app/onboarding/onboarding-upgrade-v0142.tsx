// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";
import { useWaveEnv } from "@/app/waveenv/waveenv";

const UpgradeOnboardingModal_v0_14_2_Content = () => {
    const tt = useT();
    const waveEnv = useWaveEnv();
    return (
        <div className="flex flex-col items-start w-full mb-2 unselectable">
            <div className="text-secondary leading-relaxed mb-4">
                <p className="mb-0">
                    {tt(
                        "Wave v0.14.2 introduces a new block badge system for at-a-glance status, along with directory preview improvements and bug fixes. v0.14.3 is a patch release fixing a showstopper bug in onboarding."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4 mb-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-bell"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Block & Tab Badges")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Block Badges Roll Up to Tabs")}</strong> -{" "}
                                {tt(
                                    "Blocks can display icon badges (with color and priority) that are visible in the tab bar for at-a-glance status"
                                )}
                            </li>
                            <li>
                                <strong>{tt("Bell Indicator On by Default")}</strong> -{" "}
                                {tt("Terminal bell badge now lights up the block and tab when your terminal rings (controlled by")}{" "}
                                <code>term:bellindicator</code>)
                            </li>
                            <li>
                                <strong>
                                    <code>wsh badge</code>
                                </strong>{" "}
                                - {tt("New command to set or clear badges from the CLI. Supports icons, colors, priorities, and PID-linked badges")}
                            </li>
                            <li>
                                <strong>{tt("Claude Code Integration")}</strong> - {tt("Use")} <code>wsh badge</code>{" "}
                                {tt("with Claude Code hooks to surface AI task status as tab bar notifications")}{" "}
                                <button
                                    onClick={() =>
                                        waveEnv.electron.openExternal("https://docs.waveterm.dev/claude-code")
                                    }
                                    className="text-accent text-sm font-normal cursor-pointer hover:underline"
                                >
                                    {tt("[see docs]")}
                                </button>
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-folder-open"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Other Changes")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>[v0.14.3] </strong>[bugfix] {tt("Fixed a showstopper onboarding bug")}
                            </li>
                            <li>
                                <strong>{tt("Directory Preview")}</strong> -{" "}
                                {tt(
                                    "Improved mod time formatting, zebra-striped rows, better default sort, and YAML file support"
                                )}
                            </li>
                            <li>
                                <strong>{tt("Search Bar")}</strong> - {tt("Clipboard and focus improvements")}
                            </li>
                            <li>[bugfix] {tt('Fixed "New Window" hanging on GNOME desktops')}</li>
                            <li>[bugfix] {tt('Fixed "Save Session As..." focused window tracking bug')}</li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_14_2_Content.displayName = "UpgradeOnboardingModal_v0_14_2_Content";

export { UpgradeOnboardingModal_v0_14_2_Content };