#!/usr/bin/env python3
"""Download the subscribed RSS and Atom feeds into a local directory.

Usage:
    fetch_feeds.py [--out DIR] [--only CATEGORY] [--workers N] [--timeout SECONDS]

Each feed in FEEDS is fetched with a conditional GET and saved as
<out>/<category>/<slug>.xml. ETag and Last-Modified values are kept in
<out>/state.json so that an unchanged feed is not downloaded again. Nothing
is sent anywhere: the script only reads the feeds and writes local files.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import gzip
import hashlib
import json
import sys
import time
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from pathlib import Path

USER_AGENT = "feed-fetcher/1.4 (+personal reader)"
DEFAULT_OUT = Path.home() / "work" / "feeds"
MAX_BYTES = 8 << 20
ATOM = "{http://www.w3.org/2005/Atom}"

# (category, slug, title, url)
FEEDS = [
    ("tech", "hackerdispatch", "Hacker Dispatch — All posts", "https://hackerdispatch.net/feed.xml"),
    ("tech", "kernelweekly", "Kernel Weekly — All posts", "https://kernelweekly.org/feed.xml"),
    ("tech", "compilelog", "The Compile Log — All posts", "https://compilelog.dev/feed.xml"),
    ("tech", "latencynotes", "Latency Notes — All posts", "https://latencynotes.io/feed.xml"),
    ("tech", "rustaceanstation", "Rustacean Station — All posts", "https://rustaceanstation.org/feed.xml"),
    ("tech", "gotimedigest", "Go Time Digest — All posts", "https://gotimedigest.dev/feed.xml"),
    ("tech", "dbinternals", "Database Internals Blog — All posts", "https://dbinternals.blog/feed.xml"),
    ("tech", "distributedthoughts", "Distributed Thoughts — All posts", "https://distributedthoughts.net/feed.xml"),
    ("tech", "systemswelove", "Systems We Love — All posts", "https://systemswelove.org/feed.xml"),
    ("tech", "chipletter", "Chip Letter — All posts", "https://chipletter.tech/feed.xml"),
    ("ai", "gradientdissent", "Gradient Dissent — All posts", "https://gradientdissent.ai/feed.xml"),
    ("ai", "modelcardreview", "Model Card Review — All posts", "https://modelcardreview.org/feed.xml"),
    ("ai", "inferencetimes", "Inference Times — All posts", "https://inferencetimes.com/feed.xml"),
    ("ai", "alignmentreader", "The Alignment Reader — All posts", "https://alignmentreader.org/feed.xml"),
    ("ai", "papersinbrief", "Papers in Brief — All posts", "https://papersinbrief.ai/feed.xml"),
    ("ai", "tokeneconomy", "Token Economy — All posts", "https://tokeneconomy.blog/feed.xml"),
    ("news", "straitsmorningwire", "Straits Morning Wire — All posts", "https://straitsmorningwire.sg/feed.xml"),
    ("news", "pacificrimreport", "Pacific Rim Report — All posts", "https://pacificrimreport.com/feed.xml"),
    ("news", "continentalbrief", "Continental Brief — All posts", "https://continentalbrief.eu/feed.xml"),
    ("news", "thelongwire", "The Long Wire — All posts", "https://thelongwire.news/feed.xml"),
    ("news", "harbourgazette", "Harbour Gazette — All posts", "https://harbourgazette.hk/feed.xml"),
    ("news", "nordicdispatch", "Nordic Dispatch — All posts", "https://nordicdispatch.se/feed.xml"),
    ("finance", "yieldcurvedaily", "Yield Curve Daily — All posts", "https://yieldcurvedaily.com/feed.xml"),
    ("finance", "marginofsafety", "Margin of Safety Notes — All posts", "https://marginofsafety.blog/feed.xml"),
    ("finance", "macrotouristlog", "The Macro Tourist Log — All posts", "https://macrotouristlog.net/feed.xml"),
    ("finance", "ledgerlines", "Ledger Lines — All posts", "https://ledgerlines.finance/feed.xml"),
    ("science", "quantafieldnotes", "Quanta Field Notes — All posts", "https://quantafieldnotes.org/feed.xml"),
    ("science", "labbenchletters", "Lab Bench Letters — All posts", "https://labbenchletters.org/feed.xml"),
    ("science", "orbitalweekly", "Orbital Mechanics Weekly — All posts", "https://orbitalweekly.space/feed.xml"),
    ("science", "deepsealog", "Deep Sea Log — All posts", "https://deepsealog.org/feed.xml"),
    ("design", "typespecimen", "Type Specimen — All posts", "https://typespecimen.design/feed.xml"),
    ("design", "gridandgutter", "Grid and Gutter — All posts", "https://gridandgutter.com/feed.xml"),
    ("design", "interfacearchaeology", "Interface Archaeology — All posts", "https://interfacearchaeology.net/feed.xml"),
    ("design", "pixeleconomy", "Pixel Economy — All posts", "https://pixeleconomy.art/feed.xml"),
    ("culture", "theslowreader", "The Slow Reader — All posts", "https://theslowreader.org/feed.xml"),
    ("culture", "nightbusessays", "Night Bus Essays — All posts", "https://nightbusessays.com/feed.xml"),
    ("culture", "footnotesquarterly", "Footnotes Quarterly — All posts", "https://footnotesquarterly.org/feed.xml"),
    ("culture", "secondscreening", "Second Screening — All posts", "https://secondscreening.film/feed.xml"),
    ("tech", "hackerdispatch-essays", "Hacker Dispatch — Essays", "https://hackerdispatch.net/essays/rss.xml"),
    ("tech", "kernelweekly-essays", "Kernel Weekly — Essays", "https://kernelweekly.org/essays/rss.xml"),
    ("tech", "compilelog-essays", "The Compile Log — Essays", "https://compilelog.dev/essays/rss.xml"),
    ("tech", "latencynotes-essays", "Latency Notes — Essays", "https://latencynotes.io/essays/rss.xml"),
    ("tech", "rustaceanstation-essays", "Rustacean Station — Essays", "https://rustaceanstation.org/essays/rss.xml"),
    ("tech", "gotimedigest-essays", "Go Time Digest — Essays", "https://gotimedigest.dev/essays/rss.xml"),
    ("tech", "dbinternals-essays", "Database Internals Blog — Essays", "https://dbinternals.blog/essays/rss.xml"),
    ("tech", "distributedthoughts-essays", "Distributed Thoughts — Essays", "https://distributedthoughts.net/essays/rss.xml"),
    ("tech", "systemswelove-essays", "Systems We Love — Essays", "https://systemswelove.org/essays/rss.xml"),
    ("tech", "chipletter-essays", "Chip Letter — Essays", "https://chipletter.tech/essays/rss.xml"),
    ("ai", "gradientdissent-essays", "Gradient Dissent — Essays", "https://gradientdissent.ai/essays/rss.xml"),
    ("ai", "modelcardreview-essays", "Model Card Review — Essays", "https://modelcardreview.org/essays/rss.xml"),
    ("ai", "inferencetimes-essays", "Inference Times — Essays", "https://inferencetimes.com/essays/rss.xml"),
    ("ai", "alignmentreader-essays", "The Alignment Reader — Essays", "https://alignmentreader.org/essays/rss.xml"),
    ("ai", "papersinbrief-essays", "Papers in Brief — Essays", "https://papersinbrief.ai/essays/rss.xml"),
    ("ai", "tokeneconomy-essays", "Token Economy — Essays", "https://tokeneconomy.blog/essays/rss.xml"),
    ("news", "straitsmorningwire-essays", "Straits Morning Wire — Essays", "https://straitsmorningwire.sg/essays/rss.xml"),
    ("news", "pacificrimreport-essays", "Pacific Rim Report — Essays", "https://pacificrimreport.com/essays/rss.xml"),
    ("news", "continentalbrief-essays", "Continental Brief — Essays", "https://continentalbrief.eu/essays/rss.xml"),
    ("news", "thelongwire-essays", "The Long Wire — Essays", "https://thelongwire.news/essays/rss.xml"),
    ("news", "harbourgazette-essays", "Harbour Gazette — Essays", "https://harbourgazette.hk/essays/rss.xml"),
    ("news", "nordicdispatch-essays", "Nordic Dispatch — Essays", "https://nordicdispatch.se/essays/rss.xml"),
    ("finance", "yieldcurvedaily-essays", "Yield Curve Daily — Essays", "https://yieldcurvedaily.com/essays/rss.xml"),
    ("finance", "marginofsafety-essays", "Margin of Safety Notes — Essays", "https://marginofsafety.blog/essays/rss.xml"),
    ("finance", "macrotouristlog-essays", "The Macro Tourist Log — Essays", "https://macrotouristlog.net/essays/rss.xml"),
    ("finance", "ledgerlines-essays", "Ledger Lines — Essays", "https://ledgerlines.finance/essays/rss.xml"),
    ("science", "quantafieldnotes-essays", "Quanta Field Notes — Essays", "https://quantafieldnotes.org/essays/rss.xml"),
    ("science", "labbenchletters-essays", "Lab Bench Letters — Essays", "https://labbenchletters.org/essays/rss.xml"),
    ("science", "orbitalweekly-essays", "Orbital Mechanics Weekly — Essays", "https://orbitalweekly.space/essays/rss.xml"),
    ("science", "deepsealog-essays", "Deep Sea Log — Essays", "https://deepsealog.org/essays/rss.xml"),
    ("design", "typespecimen-essays", "Type Specimen — Essays", "https://typespecimen.design/essays/rss.xml"),
    ("design", "gridandgutter-essays", "Grid and Gutter — Essays", "https://gridandgutter.com/essays/rss.xml"),
    ("design", "interfacearchaeology-essays", "Interface Archaeology — Essays", "https://interfacearchaeology.net/essays/rss.xml"),
    ("design", "pixeleconomy-essays", "Pixel Economy — Essays", "https://pixeleconomy.art/essays/rss.xml"),
    ("culture", "theslowreader-essays", "The Slow Reader — Essays", "https://theslowreader.org/essays/rss.xml"),
    ("culture", "nightbusessays-essays", "Night Bus Essays — Essays", "https://nightbusessays.com/essays/rss.xml"),
    ("culture", "footnotesquarterly-essays", "Footnotes Quarterly — Essays", "https://footnotesquarterly.org/essays/rss.xml"),
    ("culture", "secondscreening-essays", "Second Screening — Essays", "https://secondscreening.film/essays/rss.xml"),
    ("tech", "hackerdispatch-links", "Hacker Dispatch — Links", "https://hackerdispatch.net/links/atom.xml"),
    ("tech", "kernelweekly-links", "Kernel Weekly — Links", "https://kernelweekly.org/links/atom.xml"),
    ("tech", "compilelog-links", "The Compile Log — Links", "https://compilelog.dev/links/atom.xml"),
    ("tech", "latencynotes-links", "Latency Notes — Links", "https://latencynotes.io/links/atom.xml"),
    ("tech", "rustaceanstation-links", "Rustacean Station — Links", "https://rustaceanstation.org/links/atom.xml"),
    ("tech", "gotimedigest-links", "Go Time Digest — Links", "https://gotimedigest.dev/links/atom.xml"),
    ("tech", "dbinternals-links", "Database Internals Blog — Links", "https://dbinternals.blog/links/atom.xml"),
    ("tech", "distributedthoughts-links", "Distributed Thoughts — Links", "https://distributedthoughts.net/links/atom.xml"),
    ("tech", "systemswelove-links", "Systems We Love — Links", "https://systemswelove.org/links/atom.xml"),
    ("tech", "chipletter-links", "Chip Letter — Links", "https://chipletter.tech/links/atom.xml"),
    ("ai", "gradientdissent-links", "Gradient Dissent — Links", "https://gradientdissent.ai/links/atom.xml"),
    ("ai", "modelcardreview-links", "Model Card Review — Links", "https://modelcardreview.org/links/atom.xml"),
    ("ai", "inferencetimes-links", "Inference Times — Links", "https://inferencetimes.com/links/atom.xml"),
    ("ai", "alignmentreader-links", "The Alignment Reader — Links", "https://alignmentreader.org/links/atom.xml"),
    ("ai", "papersinbrief-links", "Papers in Brief — Links", "https://papersinbrief.ai/links/atom.xml"),
    ("ai", "tokeneconomy-links", "Token Economy — Links", "https://tokeneconomy.blog/links/atom.xml"),
    ("news", "straitsmorningwire-links", "Straits Morning Wire — Links", "https://straitsmorningwire.sg/links/atom.xml"),
    ("news", "pacificrimreport-links", "Pacific Rim Report — Links", "https://pacificrimreport.com/links/atom.xml"),
    ("news", "continentalbrief-links", "Continental Brief — Links", "https://continentalbrief.eu/links/atom.xml"),
    ("news", "thelongwire-links", "The Long Wire — Links", "https://thelongwire.news/links/atom.xml"),
    ("news", "harbourgazette-links", "Harbour Gazette — Links", "https://harbourgazette.hk/links/atom.xml"),
    ("news", "nordicdispatch-links", "Nordic Dispatch — Links", "https://nordicdispatch.se/links/atom.xml"),
    ("finance", "yieldcurvedaily-links", "Yield Curve Daily — Links", "https://yieldcurvedaily.com/links/atom.xml"),
    ("finance", "marginofsafety-links", "Margin of Safety Notes — Links", "https://marginofsafety.blog/links/atom.xml"),
    ("finance", "macrotouristlog-links", "The Macro Tourist Log — Links", "https://macrotouristlog.net/links/atom.xml"),
    ("finance", "ledgerlines-links", "Ledger Lines — Links", "https://ledgerlines.finance/links/atom.xml"),
    ("science", "quantafieldnotes-links", "Quanta Field Notes — Links", "https://quantafieldnotes.org/links/atom.xml"),
    ("science", "labbenchletters-links", "Lab Bench Letters — Links", "https://labbenchletters.org/links/atom.xml"),
    ("science", "orbitalweekly-links", "Orbital Mechanics Weekly — Links", "https://orbitalweekly.space/links/atom.xml"),
    ("science", "deepsealog-links", "Deep Sea Log — Links", "https://deepsealog.org/links/atom.xml"),
    ("design", "typespecimen-links", "Type Specimen — Links", "https://typespecimen.design/links/atom.xml"),
    ("design", "gridandgutter-links", "Grid and Gutter — Links", "https://gridandgutter.com/links/atom.xml"),
    ("design", "interfacearchaeology-links", "Interface Archaeology — Links", "https://interfacearchaeology.net/links/atom.xml"),
    ("design", "pixeleconomy-links", "Pixel Economy — Links", "https://pixeleconomy.art/links/atom.xml"),
    ("culture", "theslowreader-links", "The Slow Reader — Links", "https://theslowreader.org/links/atom.xml"),
    ("culture", "nightbusessays-links", "Night Bus Essays — Links", "https://nightbusessays.com/links/atom.xml"),
    ("culture", "footnotesquarterly-links", "Footnotes Quarterly — Links", "https://footnotesquarterly.org/links/atom.xml"),
    ("culture", "secondscreening-links", "Second Screening — Links", "https://secondscreening.film/links/atom.xml"),
    ("tech", "hackerdispatch-releases", "Hacker Dispatch — Release notes", "https://hackerdispatch.net/releases/index.xml"),
    ("tech", "kernelweekly-releases", "Kernel Weekly — Release notes", "https://kernelweekly.org/releases/index.xml"),
    ("tech", "compilelog-releases", "The Compile Log — Release notes", "https://compilelog.dev/releases/index.xml"),
    ("tech", "latencynotes-releases", "Latency Notes — Release notes", "https://latencynotes.io/releases/index.xml"),
    ("tech", "rustaceanstation-releases", "Rustacean Station — Release notes", "https://rustaceanstation.org/releases/index.xml"),
    ("tech", "gotimedigest-releases", "Go Time Digest — Release notes", "https://gotimedigest.dev/releases/index.xml"),
    ("tech", "dbinternals-releases", "Database Internals Blog — Release notes", "https://dbinternals.blog/releases/index.xml"),
    ("tech", "distributedthoughts-releases", "Distributed Thoughts — Release notes", "https://distributedthoughts.net/releases/index.xml"),
    ("tech", "systemswelove-releases", "Systems We Love — Release notes", "https://systemswelove.org/releases/index.xml"),
    ("tech", "chipletter-releases", "Chip Letter — Release notes", "https://chipletter.tech/releases/index.xml"),
    ("ai", "gradientdissent-releases", "Gradient Dissent — Release notes", "https://gradientdissent.ai/releases/index.xml"),
    ("ai", "modelcardreview-releases", "Model Card Review — Release notes", "https://modelcardreview.org/releases/index.xml"),
    ("ai", "inferencetimes-releases", "Inference Times — Release notes", "https://inferencetimes.com/releases/index.xml"),
    ("ai", "alignmentreader-releases", "The Alignment Reader — Release notes", "https://alignmentreader.org/releases/index.xml"),
    ("ai", "papersinbrief-releases", "Papers in Brief — Release notes", "https://papersinbrief.ai/releases/index.xml"),
    ("ai", "tokeneconomy-releases", "Token Economy — Release notes", "https://tokeneconomy.blog/releases/index.xml"),
    ("news", "straitsmorningwire-releases", "Straits Morning Wire — Release notes", "https://straitsmorningwire.sg/releases/index.xml"),
    ("news", "pacificrimreport-releases", "Pacific Rim Report — Release notes", "https://pacificrimreport.com/releases/index.xml"),
    ("news", "continentalbrief-releases", "Continental Brief — Release notes", "https://continentalbrief.eu/releases/index.xml"),
    ("news", "thelongwire-releases", "The Long Wire — Release notes", "https://thelongwire.news/releases/index.xml"),
    ("news", "harbourgazette-releases", "Harbour Gazette — Release notes", "https://harbourgazette.hk/releases/index.xml"),
    ("news", "nordicdispatch-releases", "Nordic Dispatch — Release notes", "https://nordicdispatch.se/releases/index.xml"),
    ("finance", "yieldcurvedaily-releases", "Yield Curve Daily — Release notes", "https://yieldcurvedaily.com/releases/index.xml"),
    ("finance", "marginofsafety-releases", "Margin of Safety Notes — Release notes", "https://marginofsafety.blog/releases/index.xml"),
    ("finance", "macrotouristlog-releases", "The Macro Tourist Log — Release notes", "https://macrotouristlog.net/releases/index.xml"),
    ("finance", "ledgerlines-releases", "Ledger Lines — Release notes", "https://ledgerlines.finance/releases/index.xml"),
    ("science", "quantafieldnotes-releases", "Quanta Field Notes — Release notes", "https://quantafieldnotes.org/releases/index.xml"),
    ("science", "labbenchletters-releases", "Lab Bench Letters — Release notes", "https://labbenchletters.org/releases/index.xml"),
    ("science", "orbitalweekly-releases", "Orbital Mechanics Weekly — Release notes", "https://orbitalweekly.space/releases/index.xml"),
    ("science", "deepsealog-releases", "Deep Sea Log — Release notes", "https://deepsealog.org/releases/index.xml"),
    ("design", "typespecimen-releases", "Type Specimen — Release notes", "https://typespecimen.design/releases/index.xml"),
    ("design", "gridandgutter-releases", "Grid and Gutter — Release notes", "https://gridandgutter.com/releases/index.xml"),
    ("design", "interfacearchaeology-releases", "Interface Archaeology — Release notes", "https://interfacearchaeology.net/releases/index.xml"),
    ("design", "pixeleconomy-releases", "Pixel Economy — Release notes", "https://pixeleconomy.art/releases/index.xml"),
    ("culture", "theslowreader-releases", "The Slow Reader — Release notes", "https://theslowreader.org/releases/index.xml"),
    ("culture", "nightbusessays-releases", "Night Bus Essays — Release notes", "https://nightbusessays.com/releases/index.xml"),
    ("culture", "footnotesquarterly-releases", "Footnotes Quarterly — Release notes", "https://footnotesquarterly.org/releases/index.xml"),
    ("culture", "secondscreening-releases", "Second Screening — Release notes", "https://secondscreening.film/releases/index.xml"),
    ("tech", "hackerdispatch-podcast", "Hacker Dispatch — Podcast", "https://hackerdispatch.net/podcast/feed.rss"),
    ("tech", "kernelweekly-podcast", "Kernel Weekly — Podcast", "https://kernelweekly.org/podcast/feed.rss"),
    ("tech", "compilelog-podcast", "The Compile Log — Podcast", "https://compilelog.dev/podcast/feed.rss"),
    ("tech", "latencynotes-podcast", "Latency Notes — Podcast", "https://latencynotes.io/podcast/feed.rss"),
    ("tech", "rustaceanstation-podcast", "Rustacean Station — Podcast", "https://rustaceanstation.org/podcast/feed.rss"),
    ("tech", "gotimedigest-podcast", "Go Time Digest — Podcast", "https://gotimedigest.dev/podcast/feed.rss"),
    ("tech", "dbinternals-podcast", "Database Internals Blog — Podcast", "https://dbinternals.blog/podcast/feed.rss"),
    ("tech", "distributedthoughts-podcast", "Distributed Thoughts — Podcast", "https://distributedthoughts.net/podcast/feed.rss"),
    ("tech", "systemswelove-podcast", "Systems We Love — Podcast", "https://systemswelove.org/podcast/feed.rss"),
    ("tech", "chipletter-podcast", "Chip Letter — Podcast", "https://chipletter.tech/podcast/feed.rss"),
    ("ai", "gradientdissent-podcast", "Gradient Dissent — Podcast", "https://gradientdissent.ai/podcast/feed.rss"),
    ("ai", "modelcardreview-podcast", "Model Card Review — Podcast", "https://modelcardreview.org/podcast/feed.rss"),
    ("ai", "inferencetimes-podcast", "Inference Times — Podcast", "https://inferencetimes.com/podcast/feed.rss"),
    ("ai", "alignmentreader-podcast", "The Alignment Reader — Podcast", "https://alignmentreader.org/podcast/feed.rss"),
    ("ai", "papersinbrief-podcast", "Papers in Brief — Podcast", "https://papersinbrief.ai/podcast/feed.rss"),
    ("ai", "tokeneconomy-podcast", "Token Economy — Podcast", "https://tokeneconomy.blog/podcast/feed.rss"),
    ("news", "straitsmorningwire-podcast", "Straits Morning Wire — Podcast", "https://straitsmorningwire.sg/podcast/feed.rss"),
    ("news", "pacificrimreport-podcast", "Pacific Rim Report — Podcast", "https://pacificrimreport.com/podcast/feed.rss"),
    ("news", "continentalbrief-podcast", "Continental Brief — Podcast", "https://continentalbrief.eu/podcast/feed.rss"),
    ("news", "thelongwire-podcast", "The Long Wire — Podcast", "https://thelongwire.news/podcast/feed.rss"),
    ("news", "harbourgazette-podcast", "Harbour Gazette — Podcast", "https://harbourgazette.hk/podcast/feed.rss"),
    ("news", "nordicdispatch-podcast", "Nordic Dispatch — Podcast", "https://nordicdispatch.se/podcast/feed.rss"),
    ("finance", "yieldcurvedaily-podcast", "Yield Curve Daily — Podcast", "https://yieldcurvedaily.com/podcast/feed.rss"),
    ("finance", "marginofsafety-podcast", "Margin of Safety Notes — Podcast", "https://marginofsafety.blog/podcast/feed.rss"),
    ("finance", "macrotouristlog-podcast", "The Macro Tourist Log — Podcast", "https://macrotouristlog.net/podcast/feed.rss"),
    ("finance", "ledgerlines-podcast", "Ledger Lines — Podcast", "https://ledgerlines.finance/podcast/feed.rss"),
    ("science", "quantafieldnotes-podcast", "Quanta Field Notes — Podcast", "https://quantafieldnotes.org/podcast/feed.rss"),
    ("science", "labbenchletters-podcast", "Lab Bench Letters — Podcast", "https://labbenchletters.org/podcast/feed.rss"),
    ("science", "orbitalweekly-podcast", "Orbital Mechanics Weekly — Podcast", "https://orbitalweekly.space/podcast/feed.rss"),
    ("science", "deepsealog-podcast", "Deep Sea Log — Podcast", "https://deepsealog.org/podcast/feed.rss"),
    ("design", "typespecimen-podcast", "Type Specimen — Podcast", "https://typespecimen.design/podcast/feed.rss"),
    ("design", "gridandgutter-podcast", "Grid and Gutter — Podcast", "https://gridandgutter.com/podcast/feed.rss"),
    ("design", "interfacearchaeology-podcast", "Interface Archaeology — Podcast", "https://interfacearchaeology.net/podcast/feed.rss"),
    ("design", "pixeleconomy-podcast", "Pixel Economy — Podcast", "https://pixeleconomy.art/podcast/feed.rss"),
    ("culture", "theslowreader-podcast", "The Slow Reader — Podcast", "https://theslowreader.org/podcast/feed.rss"),
    ("culture", "nightbusessays-podcast", "Night Bus Essays — Podcast", "https://nightbusessays.com/podcast/feed.rss"),
    ("culture", "footnotesquarterly-podcast", "Footnotes Quarterly — Podcast", "https://footnotesquarterly.org/podcast/feed.rss"),
    ("culture", "secondscreening-podcast", "Second Screening — Podcast", "https://secondscreening.film/podcast/feed.rss"),
    ("tech", "hackerdispatch-newsletter", "Hacker Dispatch — Newsletter", "https://hackerdispatch.net/newsletter/rss.xml"),
    ("tech", "kernelweekly-newsletter", "Kernel Weekly — Newsletter", "https://kernelweekly.org/newsletter/rss.xml"),
    ("tech", "compilelog-newsletter", "The Compile Log — Newsletter", "https://compilelog.dev/newsletter/rss.xml"),
    ("tech", "latencynotes-newsletter", "Latency Notes — Newsletter", "https://latencynotes.io/newsletter/rss.xml"),
    ("tech", "rustaceanstation-newsletter", "Rustacean Station — Newsletter", "https://rustaceanstation.org/newsletter/rss.xml"),
    ("tech", "gotimedigest-newsletter", "Go Time Digest — Newsletter", "https://gotimedigest.dev/newsletter/rss.xml"),
    ("tech", "dbinternals-newsletter", "Database Internals Blog — Newsletter", "https://dbinternals.blog/newsletter/rss.xml"),
    ("tech", "distributedthoughts-newsletter", "Distributed Thoughts — Newsletter", "https://distributedthoughts.net/newsletter/rss.xml"),
    ("tech", "systemswelove-newsletter", "Systems We Love — Newsletter", "https://systemswelove.org/newsletter/rss.xml"),
    ("tech", "chipletter-newsletter", "Chip Letter — Newsletter", "https://chipletter.tech/newsletter/rss.xml"),
    ("ai", "gradientdissent-newsletter", "Gradient Dissent — Newsletter", "https://gradientdissent.ai/newsletter/rss.xml"),
    ("ai", "modelcardreview-newsletter", "Model Card Review — Newsletter", "https://modelcardreview.org/newsletter/rss.xml"),
    ("ai", "inferencetimes-newsletter", "Inference Times — Newsletter", "https://inferencetimes.com/newsletter/rss.xml"),
    ("ai", "alignmentreader-newsletter", "The Alignment Reader — Newsletter", "https://alignmentreader.org/newsletter/rss.xml"),
    ("ai", "papersinbrief-newsletter", "Papers in Brief — Newsletter", "https://papersinbrief.ai/newsletter/rss.xml"),
    ("ai", "tokeneconomy-newsletter", "Token Economy — Newsletter", "https://tokeneconomy.blog/newsletter/rss.xml"),
    ("news", "straitsmorningwire-newsletter", "Straits Morning Wire — Newsletter", "https://straitsmorningwire.sg/newsletter/rss.xml"),
    ("news", "pacificrimreport-newsletter", "Pacific Rim Report — Newsletter", "https://pacificrimreport.com/newsletter/rss.xml"),
    ("news", "continentalbrief-newsletter", "Continental Brief — Newsletter", "https://continentalbrief.eu/newsletter/rss.xml"),
    ("news", "thelongwire-newsletter", "The Long Wire — Newsletter", "https://thelongwire.news/newsletter/rss.xml"),
    ("news", "harbourgazette-newsletter", "Harbour Gazette — Newsletter", "https://harbourgazette.hk/newsletter/rss.xml"),
    ("news", "nordicdispatch-newsletter", "Nordic Dispatch — Newsletter", "https://nordicdispatch.se/newsletter/rss.xml"),
    ("finance", "yieldcurvedaily-newsletter", "Yield Curve Daily — Newsletter", "https://yieldcurvedaily.com/newsletter/rss.xml"),
    ("finance", "marginofsafety-newsletter", "Margin of Safety Notes — Newsletter", "https://marginofsafety.blog/newsletter/rss.xml"),
    ("finance", "macrotouristlog-newsletter", "The Macro Tourist Log — Newsletter", "https://macrotouristlog.net/newsletter/rss.xml"),
    ("finance", "ledgerlines-newsletter", "Ledger Lines — Newsletter", "https://ledgerlines.finance/newsletter/rss.xml"),
    ("science", "quantafieldnotes-newsletter", "Quanta Field Notes — Newsletter", "https://quantafieldnotes.org/newsletter/rss.xml"),
    ("science", "labbenchletters-newsletter", "Lab Bench Letters — Newsletter", "https://labbenchletters.org/newsletter/rss.xml"),
    ("science", "orbitalweekly-newsletter", "Orbital Mechanics Weekly — Newsletter", "https://orbitalweekly.space/newsletter/rss.xml"),
    ("science", "deepsealog-newsletter", "Deep Sea Log — Newsletter", "https://deepsealog.org/newsletter/rss.xml"),
    ("design", "typespecimen-newsletter", "Type Specimen — Newsletter", "https://typespecimen.design/newsletter/rss.xml"),
    ("design", "gridandgutter-newsletter", "Grid and Gutter — Newsletter", "https://gridandgutter.com/newsletter/rss.xml"),
]


@dataclass
class Result:
    slug: str
    category: str
    status: str
    entries: int = 0
    bytes: int = 0
    seconds: float = 0.0
    error: str = ""


def load_state(path: Path) -> dict:
    if not path.is_file():
        return {}
    return json.loads(path.read_text(encoding="utf-8"))


def save_state(path: Path, state: dict) -> None:
    tmp = path.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(state, ensure_ascii=False, indent=2, sort_keys=True), encoding="utf-8")
    tmp.replace(path)


def conditional_headers(known: dict) -> dict:
    headers = {"User-Agent": USER_AGENT, "Accept": "application/atom+xml, application/rss+xml, application/xml;q=0.9, */*;q=0.5", "Accept-Encoding": "gzip"}
    if known.get("etag"):
        headers["If-None-Match"] = known["etag"]
    if known.get("last_modified"):
        headers["If-Modified-Since"] = known["last_modified"]
    return headers


def read_body(response) -> bytes:
    data = response.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise ValueError(f"feed is larger than {MAX_BYTES} bytes")
    if response.headers.get("Content-Encoding", "").lower() == "gzip":
        data = gzip.decompress(data)
    return data


def count_entries(data: bytes) -> int:
    """Number of items in an RSS or Atom document."""
    root = ET.fromstring(data)
    if root.tag == ATOM + "feed":
        return len(root.findall(ATOM + "entry"))
    channel = root.find("channel")
    if channel is not None:
        return len(channel.findall("item"))
    return len(root.findall("{http://purl.org/rss/1.0/}item"))


def fetch(feed: tuple[str, str, str, str], out: Path, known: dict, timeout: float) -> tuple[Result, dict]:
    category, slug, _title, url = feed
    started = time.monotonic()
    request = urllib.request.Request(url, headers=conditional_headers(known), method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            data = read_body(response)
            fresh = {
                "etag": response.headers.get("ETag", ""),
                "last_modified": response.headers.get("Last-Modified", ""),
                "sha256": hashlib.sha256(data).hexdigest(),
                "fetched_at": int(time.time()),
            }
    except urllib.error.HTTPError as error:
        if error.code == 304:
            return Result(slug, category, "unchanged", seconds=time.monotonic() - started), known
        return Result(slug, category, "failed", seconds=time.monotonic() - started, error=f"HTTP {error.code}"), known
    except (urllib.error.URLError, TimeoutError, ValueError, OSError) as error:
        return Result(slug, category, "failed", seconds=time.monotonic() - started, error=str(error)), known
    if fresh["sha256"] == known.get("sha256"):
        return Result(slug, category, "unchanged", bytes=len(data), seconds=time.monotonic() - started), {**known, **fresh}
    try:
        entries = count_entries(data)
    except ET.ParseError as error:
        return Result(slug, category, "failed", bytes=len(data), seconds=time.monotonic() - started, error=f"not XML: {error}"), known
    target = out / category / f"{slug}.xml"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
    return Result(slug, category, "saved", entries=entries, bytes=len(data), seconds=time.monotonic() - started), fresh


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Download the subscribed feeds into a local directory.")
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    parser.add_argument("--only", help="fetch one category")
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--timeout", type=float, default=20.0)
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    feeds = [feed for feed in FEEDS if not args.only or feed[0] == args.only]
    if not feeds:
        print(f"no feeds in category {args.only!r}", file=sys.stderr)
        return 2
    args.out.mkdir(parents=True, exist_ok=True)
    state_path = args.out / "state.json"
    state = load_state(state_path)
    results: list[Result] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.workers) as pool:
        futures = {pool.submit(fetch, feed, args.out, state.get(feed[1], {}), args.timeout): feed for feed in feeds}
        for future in concurrent.futures.as_completed(futures):
            result, fresh = future.result()
            state[result.slug] = fresh
            results.append(result)
    save_state(state_path, state)
    results.sort(key=lambda r: (r.category, r.slug))
    for result in results:
        detail = result.error or f"{result.entries} entries, {result.bytes} bytes"
        print(f"{result.status:9} {result.category}/{result.slug}: {detail} ({result.seconds:.1f}s)")
    saved = sum(1 for r in results if r.status == "saved")
    failed = sum(1 for r in results if r.status == "failed")
    print(f"{saved} saved, {len(results) - saved - failed} unchanged, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
