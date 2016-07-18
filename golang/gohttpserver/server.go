package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/russross/blackfriday"
)

var confPort string
var confRoot string
var confIp string

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func sizeString(size int64) string {
	us := "BKMGT"
	r := float64(size)
	i := 0
	for ; r > 1000 && i < 5; i++ {
		r = r / 1024
	}
	return fmt.Sprintf("%.2f %c", r, us[i])
}

func listDir(rw http.ResponseWriter, req *http.Request, fpath string) {
	fmt.Fprintln(rw, html)
	fmt.Fprintf(rw, "<script>start(\"【%s】\");</script>\n", req.Host+req.URL.Path)
	root, _ := filepath.Abs(confRoot)
	flist, _ := ioutil.ReadDir(fpath)
	var files, dirs []os.FileInfo
	for _, f := range flist {
		if f.IsDir() {
			dirs = append(dirs, f)
		} else {
			files = append(files, f)
		}
	}
	if root != fpath {
		fmt.Fprintf(rw, "<script>addRow(\"..\",\"..\",1,0,\"0 B\", 0,\"\");</script>\n")
	}
	for _, item := range dirs {
		encoded := strings.Replace(url.QueryEscape(item.Name()), "+", "%20", -1)
		fmt.Fprintf(rw, "<script>addRow(\"%s\",\"%s\",1,0,\"0 B\", %d,\"%s\");</script>\n",
			item.Name(), encoded, item.ModTime().Unix(), item.ModTime().Format("2006-01-02 15:04:05"))
	}
	for _, item := range files {
		encoded := strings.Replace(url.QueryEscape(item.Name()), "+", "%20", -1)
		fmt.Fprintf(rw, "<script>addRow(\"%s\",\"%s\",0,%d,\"%s\", %d,\"%s\");</script>\n",
			item.Name(), encoded, item.Size(), sizeString(item.Size()), item.ModTime().Unix(), item.ModTime().Format("2006-01-02 15:04:05"))
	}
}

func markdownHandler(rw http.ResponseWriter, req *http.Request, fpath string) {

	htmlOpt := 0 |
		blackfriday.HTML_TOC |
		blackfriday.HTML_USE_XHTML |
		blackfriday.HTML_USE_SMARTYPANTS |
		blackfriday.HTML_SMARTYPANTS_FRACTIONS |
		blackfriday.HTML_SMARTYPANTS_DASHES |
		blackfriday.HTML_SMARTYPANTS_LATEX_DASHES |
		blackfriday.HTML_SMARTYPANTS_ANGLED_QUOTES |
		blackfriday.HTML_FOOTNOTE_RETURN_LINKS
	renderOpt := 0 |
		blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
		blackfriday.EXTENSION_TABLES |
		blackfriday.EXTENSION_AUTOLINK |
		blackfriday.EXTENSION_FENCED_CODE |
		blackfriday.EXTENSION_STRIKETHROUGH |
		blackfriday.EXTENSION_LAX_HTML_BLOCKS |
		blackfriday.EXTENSION_FOOTNOTES |
		blackfriday.EXTENSION_HEADER_IDS |
		blackfriday.EXTENSION_TITLEBLOCK |
		blackfriday.EXTENSION_AUTO_HEADER_IDS |
		blackfriday.EXTENSION_BACKSLASH_LINE_BREAK |
		blackfriday.EXTENSION_DEFINITION_LISTS

	fd, _ := os.Open(fpath)
	defer fd.Close()
	freader := bufio.NewReader(fd)

	var title, date, author string
	var tags []string
	var line string
	var err error
	for {
		line, err = freader.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "---") {
			break
		}
		if strings.HasPrefix(line, "---title:") {
			i := strings.Index(line, ":")
			title = strings.TrimSpace(line[i+1:])
		} else if strings.HasPrefix(line, "---date:") {
			i := strings.Index(line, ":")
			date = strings.TrimSpace(line[i+1:])
		} else if strings.HasPrefix(line, "---author:") {
			i := strings.Index(line, ":")
			author = strings.TrimSpace(line[i+1:])
		} else if strings.HasPrefix(line, "---tags:") {
			i := strings.Index(line, ":")
			tags = strings.Split(strings.TrimSpace(line[i+1:]), ",")
		}
	}

	articleMeta := &bytes.Buffer{}

	pos := strings.LastIndex(req.RequestURI, "/")
	catalog := req.RequestURI[:pos+1]
	if catalog != "" {
		articleMeta.WriteString(fmt.Sprintf(`<a href="%s"><i class="fa fa-home catalog"></i></a>`, catalog))
	}

	raw := req.URL.RawPath + "?raw=1"
	articleMeta.WriteString(fmt.Sprintf(`<a href="%s"><i class="fa fa-file-code-o raw"></i></a>`, raw))

	if date != "" {
		articleMeta.WriteString(`<i class="fa fa-calendar"></i>`)
		articleMeta.WriteString(date)
	}
	if author != "" {
		articleMeta.WriteString(`<i class="fa fa-user"></i>`)
		articleMeta.WriteString(author)
	}

	if len(tags) != 0 {
		articleMeta.WriteString(`<i class="fa fa-tags"></i>`)
		var tmp []string
		for _, tag := range tags {
			tmp = append(tmp, fmt.Sprintf(`<a class="tag">%s</a>`, tag))
		}
		articleMeta.WriteString(strings.Join(tmp, " | "))
	}

	text, _ := ioutil.ReadAll(freader)
	text = append([]byte(line), text...)
	renderer := blackfriday.HtmlRenderer(htmlOpt, "", "")
	mdbody := blackfriday.MarkdownOptions(text, renderer, blackfriday.Options{
		Extensions: renderOpt})

	m := map[string]interface{}{
		"title":       title,
		"mdbody":      string(mdbody),
		"articlemeta": articleMeta.String(),
	}

	rw.Header().Set("content-type", "text/html; charset=utf-8")
	template.Must(template.New("markdown").Parse(mdTemplate)).Execute(rw, m)
}

var cssList = map[string]string{
	"/md.css": mdCss,
	"/fa.css": faCss,
}

func cssHandler(rw http.ResponseWriter, req *http.Request) {
	rw.Header().Set("content-type", "text/css; charset=utf-8")
	io.WriteString(rw, cssList[req.RequestURI])
}

func rootHandler(rw http.ResponseWriter, req *http.Request) {

	if _, ok := cssList[req.RequestURI]; ok {
		cssHandler(rw, req)
		return
	}

	fpath, _ := filepath.Abs(path.Join(confRoot, req.URL.Path))
	finfo, err := os.Stat(fpath)
	if err != nil && os.IsNotExist(err) {
		http.Error(rw, "404", http.StatusNotFound)
	} else {
		if finfo.IsDir() {
			listDir(rw, req, fpath)
		} else {
			//fmt.Println(req.RequestURI)
			if strings.HasSuffix(fpath, ".md") {
				if req.FormValue("raw") == "1" {
					http.ServeFile(rw, req, fpath)
				} else {
					markdownHandler(rw, req, fpath)
				}
			} else {
				http.ServeFile(rw, req, fpath)
			}
		}
	}
}

func main() {
	flag.StringVar(&confIp, "ip", "0.0.0.0", "listening ip")
	flag.StringVar(&confPort, "port", "80", "listening port")
	flag.StringVar(&confRoot, "root", ".", "root directory")
	flag.Parse()

	fmt.Printf("Serving HTTP on %s port %s, root: %s", confIp, confPort, confRoot)
	http.HandleFunc("/", rootHandler)
	log.Fatal(http.ListenAndServe(confIp+":"+confPort, nil))
}

var mdTemplate = `<!DOCTYPE html>
<html>
<head>
<title>
{{.title}}
</title>
<meta charset="utf-8" />
<link rel="stylesheet" type="text/css" href="/md.css"/>
<link rel="stylesheet" type="text/css" href="/fa.css"/>
</head>
<body>
{{if .articlemeta}}
<articleMeta>
{{.articlemeta}}
</articleMeta>
{{end}}
<article class="markdown-body">
{{.mdbody}}
</article>
</body>
</html>`

// html codes from chrome local file browser
var html string = `<!DOCTYPE html>

<html i18n-values="dir:textdirection;lang:language">

<head>
<meta charset="utf-8">
<meta name="google" value="notranslate">

<script>
function addRow(name, url, isdir,
    size, size_string, date_modified, date_modified_string) {
  if (name == ".")
    return;

  var root = document.location.pathname;
  if (root.substr(-1) !== "/")
    root += "/";

  var tbody = document.getElementById("tbody");
  var row = document.createElement("tr");
  var file_cell = document.createElement("td");
  var link = document.createElement("a");

  link.className = isdir ? "icon dir" : "icon file";

  if (name == "..") {
    link.href = root + "..";
    link.innerText = document.getElementById("parentDirText").innerText;
    link.className = "icon up";
    size = 0;
    size_string = "";
    date_modified = 0;
    date_modified_string = "";
  } else {
    if (isdir) {
      name = name + "/";
      url = url + "/";
      size = 0;
      size_string = "";
    } else {
      link.draggable = "true";
      link.addEventListener("dragstart", onDragStart, false);
    }
    link.innerText = name;
    link.href = root + url;
  }
  file_cell.dataset.value = name;
  file_cell.appendChild(link);

  row.appendChild(file_cell);
  row.appendChild(createCell(size, size_string));
  row.appendChild(createCell(date_modified, date_modified_string));

  tbody.appendChild(row);
}

function onDragStart(e) {
  var el = e.srcElement;
  var name = el.innerText.replace(":", "");
  var download_url_data = "application/octet-stream:" + name + ":" + el.href;
  e.dataTransfer.setData("DownloadURL", download_url_data);
  e.dataTransfer.effectAllowed = "copy";
}

function createCell(value, text) {
  var cell = document.createElement("td");
  cell.setAttribute("class", "detailsColumn");
  cell.dataset.value = value;
  cell.innerText = text;
  return cell;
}

function start(location) {
  var header = document.getElementById("header");
  header.innerText = header.innerText.replace("LOCATION", location);

  document.getElementById("title").innerText = header.innerText;
}

function onListingParsingError() {
  var box = document.getElementById("listingParsingErrorBox");
  box.innerHTML = box.innerHTML.replace("LOCATION", encodeURI(document.location)
      + "?raw");
  box.style.display = "block";
}

function sortTable(column) {
  var theader = document.getElementById("theader");
  var oldOrder = theader.cells[column].dataset.order || '1';
  oldOrder = parseInt(oldOrder, 10)
  var newOrder = 0 - oldOrder;
  theader.cells[column].dataset.order = newOrder;

  var tbody = document.getElementById("tbody");
  var rows = tbody.rows;
  var list = [], i;
  for (i = 0; i < rows.length; i++) {
    list.push(rows[i]);
  }

  list.sort(function(row1, row2) {
    var a = row1.cells[column].dataset.value;
    var b = row2.cells[column].dataset.value;
    if (column) {
      a = parseInt(a, 10);
      b = parseInt(b, 10);
      return a > b ? newOrder : a < b ? oldOrder : 0;
    }

    // Column 0 is text.
    // Also the parent directory should always be sorted at one of the ends.
    if (b == ".." | a > b) {
      return newOrder;
    } else if (a == ".." | a < b) {
      return oldOrder;
    } else {
      return 0;
    }
  });

  // Appending an existing child again just moves it.
  for (i = 0; i < list.length; i++) {
    tbody.appendChild(list[i]);
  }
}
</script>

<style>
  body {
    font-family:Consolas,Monaco,Lucida Console,monospace;
  }
  h1 {
    border-bottom: 1px solid #c0c0c0;
    margin-bottom: 10px;
    padding-bottom: 10px;
    white-space: nowrap;
  }

  table {
    border-collapse: collapse;
  }

  th {
    cursor: pointer;
  }

  td.detailsColumn {
    -webkit-padding-start: 2em;
    text-align: end;
    white-space: nowrap;
  }

  a.icon {
    -webkit-padding-start: 1.5em;
    text-decoration: none;
  }

  a.icon:hover {
    text-decoration: underline;
  }

  a.file {
    background : url("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAAABnRSTlMAAAAAAABupgeRAAABHUlEQVR42o2RMW7DIBiF3498iHRJD5JKHurL+CRVBp+i2T16tTynF2gO0KSb5ZrBBl4HHDBuK/WXACH4eO9/CAAAbdvijzLGNE1TVZXfZuHg6XCAQESAZXbOKaXO57eiKG6ft9PrKQIkCQqFoIiQFBGlFIB5nvM8t9aOX2Nd18oDzjnPgCDpn/BH4zh2XZdlWVmWiUK4IgCBoFMUz9eP6zRN75cLgEQhcmTQIbl72O0f9865qLAAsURAAgKBJKEtgLXWvyjLuFsThCSstb8rBCaAQhDYWgIZ7myM+TUBjDHrHlZcbMYYk34cN0YSLcgS+wL0fe9TXDMbY33fR2AYBvyQ8L0Gk8MwREBrTfKe4TpTzwhArXWi8HI84h/1DfwI5mhxJamFAAAAAElFTkSuQmCC ") left top no-repeat;
  }

  a.dir {
    background : url("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGXRFWHRTb2Z0d2FyZQBBZG9iZSBJbWFnZVJlYWR5ccllPAAAAd5JREFUeNqMU79rFUEQ/vbuodFEEkzAImBpkUabFP4ldpaJhZXYm/RiZWsv/hkWFglBUyTIgyAIIfgIRjHv3r39MePM7N3LcbxAFvZ2b2bn22/mm3XMjF+HL3YW7q28YSIw8mBKoBihhhgCsoORot9d3/ywg3YowMXwNde/PzGnk2vn6PitrT+/PGeNaecg4+qNY3D43vy16A5wDDd4Aqg/ngmrjl/GoN0U5V1QquHQG3q+TPDVhVwyBffcmQGJmSVfyZk7R3SngI4JKfwDJ2+05zIg8gbiereTZRHhJ5KCMOwDFLjhoBTn2g0ghagfKeIYJDPFyibJVBtTREwq60SpYvh5++PpwatHsxSm9QRLSQpEVSd7/TYJUb49TX7gztpjjEffnoVw66+Ytovs14Yp7HaKmUXeX9rKUoMoLNW3srqI5fWn8JejrVkK0QcrkFLOgS39yoKUQe292WJ1guUHG8K2o8K00oO1BTvXoW4yasclUTgZYJY9aFNfAThX5CZRmczAV52oAPoupHhWRIUUAOoyUIlYVaAa/VbLbyiZUiyFbjQFNwiZQSGl4IDy9sO5Wrty0QLKhdZPxmgGcDo8ejn+c/6eiK9poz15Kw7Dr/vN/z6W7q++091/AQYA5mZ8GYJ9K0AAAAAASUVORK5CYII= ") left top no-repeat;
  }

  a.up {
    background : url("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGXRFWHRTb2Z0d2FyZQBBZG9iZSBJbWFnZVJlYWR5ccllPAAAAmlJREFUeNpsU0toU0EUPfPysx/tTxuDH9SCWhUDooIbd7oRUUTMouqi2iIoCO6lceHWhegy4EJFinWjrlQUpVm0IIoFpVDEIthm0dpikpf3ZuZ6Z94nrXhhMjM3c8895977BBHB2PznK8WPtDgyWH5q77cPH8PpdXuhpQT4ifR9u5sfJb1bmw6VivahATDrxcRZ2njfoaMv+2j7mLDn93MPiNRMvGbL18L9IpF8h9/TN+EYkMffSiOXJ5+hkD+PdqcLpICWHOHc2CC+LEyA/K+cKQMnlQHJX8wqYG3MAJy88Wa4OLDvEqAEOpJd0LxHIMdHBziowSwVlF8D6QaicK01krw/JynwcKoEwZczewroTvZirlKJs5CqQ5CG8pb57FnJUA0LYCXMX5fibd+p8LWDDemcPZbzQyjvH+Ki1TlIciElA7ghwLKV4kRZstt2sANWRjYTAGzuP2hXZFpJ/GsxgGJ0ox1aoFWsDXyyxqCs26+ydmagFN/rRjymJ1898bzGzmQE0HCZpmk5A0RFIv8Pn0WYPsiu6t/Rsj6PauVTwffTSzGAGZhUG2F06hEc9ibS7OPMNp6ErYFlKavo7MkhmTqCxZ/jwzGA9Hx82H2BZSw1NTN9Gx8ycHkajU/7M+jInsDC7DiaEmo1bNl1AMr9ASFgqVu9MCTIzoGUimXVAnnaN0PdBBDCCYbEtMk6wkpQwIG0sn0PQIUF4GsTwLSIFKNqF6DVrQq+IWVrQDxAYQC/1SsYOI4pOxKZrfifiUSbDUisif7XlpGIPufXd/uvdvZm760M0no1FZcnrzUdjw7au3vu/BVgAFLXeuTxhTXVAAAAAElFTkSuQmCC ") left top no-repeat;
  }

  html[dir=rtl] a {
    background-position-x: right;
  }

  #listingParsingErrorBox {
    border: 1px solid black;
    background: #fae691;
    padding: 10px;
    display: none;
  }
</style>

<title id="title"></title>

</head>

<body>

<div id="listingParsingErrorBox" i18n-values=".innerHTML:listingParsingErrorBoxText"></div>

<span id="parentDirText" style="display:none" i18n-content="parentDirText"></span>

<h1 id="header" i18n-content="header"></h1>

<table>
  <thead>
    <tr class="header" id="theader">
      <th i18n-content="headerName" onclick="javascript:sortTable(0);"></th>
      <th class="detailsColumn" i18n-content="headerSize" onclick="javascript:sortTable(1);"></th>
      <th class="detailsColumn" i18n-content="headerDateModified" onclick="javascript:sortTable(2);"></th>
    </tr>
  </thead>
  <tbody id="tbody">
  </tbody>
</table>

</body>

</html>
<script>// Copyright (c) 2012 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/**
 * @fileoverview This file defines a singleton which provides access to all data
 * that is available as soon as the page's resources are loaded (before DOM
 * content has finished loading). This data includes both localized strings and
 * any data that is important to have ready from a very early stage (e.g. things
 * that must be displayed right away).
 */

/** @type {!LoadTimeData} */ var loadTimeData;

// Expose this type globally as a temporary work around until
// https://github.com/google/closure-compiler/issues/544 is fixed.
/** @constructor */
function LoadTimeData() {}

(function() {
  'use strict';

  LoadTimeData.prototype = {
    /**
     * Sets the backing object.
     *
     * Note that there is no getter for |data_| to discourage abuse of the form:
     *
     *     var value = loadTimeData.data()['key'];
     *
     * @param {Object} value The de-serialized page data.
     */
    set data(value) {
      expect(!this.data_, 'Re-setting data.');
      this.data_ = value;
    },

    /**
     * Returns a JsEvalContext for |data_|.
     * @returns {JsEvalContext}
     */
    createJsEvalContext: function() {
      return new JsEvalContext(this.data_);
    },

    /**
     * @param {string} id An ID of a value that might exist.
     * @return {boolean} True if |id| is a key in the dictionary.
     */
    valueExists: function(id) {
      return id in this.data_;
    },

    /**
     * Fetches a value, expecting that it exists.
     * @param {string} id The key that identifies the desired value.
     * @return {*} The corresponding value.
     */
    getValue: function(id) {
      expect(this.data_, 'No data. Did you remember to include strings.js?');
      var value = this.data_[id];
      expect(typeof value != 'undefined', 'Could not find value for ' + id);
      return value;
    },

    /**
     * As above, but also makes sure that the value is a string.
     * @param {string} id The key that identifies the desired string.
     * @return {string} The corresponding string value.
     */
    getString: function(id) {
      var value = this.getValue(id);
      expectIsType(id, value, 'string');
      return /** @type {string} */ (value);
    },

    /**
     * Returns a formatted localized string where $1 to $9 are replaced by the
     * second to the tenth argument.
     * @param {string} id The ID of the string we want.
     * @param {...(string|number)} var_args The extra values to include in the
     *     formatted output.
     * @return {string} The formatted string.
     */
    getStringF: function(id, var_args) {
      var value = this.getString(id);
      if (!value)
        return '';

      var varArgs = arguments;
      return value.replace(/\$[$1-9]/g, function(m) {
        return m == '$$' ? '$' : varArgs[m[1]];
      });
    },

    /**
     * As above, but also makes sure that the value is a boolean.
     * @param {string} id The key that identifies the desired boolean.
     * @return {boolean} The corresponding boolean value.
     */
    getBoolean: function(id) {
      var value = this.getValue(id);
      expectIsType(id, value, 'boolean');
      return /** @type {boolean} */ (value);
    },

    /**
     * As above, but also makes sure that the value is an integer.
     * @param {string} id The key that identifies the desired number.
     * @return {number} The corresponding number value.
     */
    getInteger: function(id) {
      var value = this.getValue(id);
      expectIsType(id, value, 'number');
      expect(value == Math.floor(value), 'Number isn\'t integer: ' + value);
      return /** @type {number} */ (value);
    },

    /**
     * Override values in loadTimeData with the values found in |replacements|.
     * @param {Object} replacements The dictionary object of keys to replace.
     */
    overrideValues: function(replacements) {
      expect(typeof replacements == 'object',
             'Replacements must be a dictionary object.');
      for (var key in replacements) {
        this.data_[key] = replacements[key];
      }
    }
  };

  /**
   * Checks condition, displays error message if expectation fails.
   * @param {*} condition The condition to check for truthiness.
   * @param {string} message The message to display if the check fails.
   */
  function expect(condition, message) {
    if (!condition) {
      console.error('Unexpected condition on ' + document.location.href + ': ' +
                    message);
    }
  }

  /**
   * Checks that the given value has the given type.
   * @param {string} id The id of the value (only used for error message).
   * @param {*} value The value to check the type on.
   * @param {string} type The type we expect |value| to be.
   */
  function expectIsType(id, value, type) {
    expect(typeof value == type, '[' + value + '] (' + id +
                                 ') is not a ' + type);
  }

  expect(!loadTimeData, 'should only include this file once');
  loadTimeData = new LoadTimeData;
})();
</script><script>loadTimeData.data = {"header":"LOCATION 的索引","headerDateModified":"修改日期","headerName":"名称","headerSize":"大小","listingParsingErrorBoxText":"糟糕！Google Chrome无法解读服务器所发送的数据。请\u003Ca href=\"http://code.google.com/p/chromium/issues/entry\">报告错误\u003C/a>，并附上\u003Ca href=\"LOCATION\">原始列表\u003C/a>。","parentDirText":"[上级目录]","textdirection":"ltr"};</script><script>// Copyright (c) 2012 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// // Copyright (c) 2012 The Chromium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

/** @typedef {Document|DocumentFragment|Element} */
var ProcessingRoot;

/**
 * @fileoverview This is a simple template engine inspired by JsTemplates
 * optimized for i18n.
 *
 * It currently supports three handlers:
 *
 *   * i18n-content which sets the textContent of the element.
 *
 *     <span i18n-content="myContent"></span>
 *
 *   * i18n-options which generates <option> elements for a <select>.
 *
 *     <select i18n-options="myOptionList"></select>
 *
 *   * i18n-values is a list of attribute-value or property-value pairs.
 *     Properties are prefixed with a '.' and can contain nested properties.
 *
 *     <span i18n-values="title:myTitle;.style.fontSize:fontSize"></span>
 *
 * This file is a copy of i18n_template.js, with minor tweaks to support using
 * load_time_data.js. It should replace i18n_template.js eventually.
 */

var i18nTemplate = (function() {
  /**
   * This provides the handlers for the templating engine. The key is used as
   * the attribute name and the value is the function that gets called for every
   * single node that has this attribute.
   * @type {!Object}
   */
  var handlers = {
    /**
     * This handler sets the textContent of the element.
     * @param {!HTMLElement} element The node to modify.
     * @param {string} key The name of the value in |data|.
     * @param {!LoadTimeData} data The data source to draw from.
     * @param {!Set<ProcessingRoot>} visited
     */
    'i18n-content': function(element, key, data, visited) {
      element.textContent = data.getString(key);
    },

    /**
     * This handler adds options to a <select> element.
     * @param {!HTMLElement} select The node to modify.
     * @param {string} key The name of the value in |data|. It should
     *     identify an array of values to initialize an <option>. Each value,
     *     if a pair, represents [content, value]. Otherwise, it should be a
     *     content string with no value.
     * @param {!LoadTimeData} data The data source to draw from.
     * @param {!Set<ProcessingRoot>} visited
     */
    'i18n-options': function(select, key, data, visited) {
      var options = data.getValue(key);
      options.forEach(function(optionData) {
        var option = typeof optionData == 'string' ?
            new Option(optionData) :
            new Option(optionData[1], optionData[0]);
        select.appendChild(option);
      });
    },

    /**
     * This is used to set HTML attributes and DOM properties. The syntax is:
     *   attributename:key;
     *   .domProperty:key;
     *   .nested.dom.property:key
     * @param {!HTMLElement} element The node to modify.
     * @param {string} attributeAndKeys The path of the attribute to modify
     *     followed by a colon, and the name of the value in |data|.
     *     Multiple attribute/key pairs may be separated by semicolons.
     * @param {!LoadTimeData} data The data source to draw from.
     * @param {!Set<ProcessingRoot>} visited
     */
    'i18n-values': function(element, attributeAndKeys, data, visited) {
      var parts = attributeAndKeys.replace(/\s/g, '').split(/;/);
      parts.forEach(function(part) {
        if (!part)
          return;

        var attributeAndKeyPair = part.match(/^([^:]+):(.+)$/);
        if (!attributeAndKeyPair)
          throw new Error('malformed i18n-values: ' + attributeAndKeys);

        var propName = attributeAndKeyPair[1];
        var propExpr = attributeAndKeyPair[2];

        var value = data.getValue(propExpr);

        // Allow a property of the form '.foo.bar' to assign a value into
        // element.foo.bar.
        if (propName[0] == '.') {
          var path = propName.slice(1).split('.');
          var targetObject = element;
          while (targetObject && path.length > 1) {
            targetObject = targetObject[path.shift()];
          }
          if (targetObject) {
            targetObject[path] = value;
            // In case we set innerHTML (ignoring others) we need to recursively
            // check the content.
            if (path == 'innerHTML') {
              for (var i = 0; i < element.children.length; ++i) {
                processWithoutCycles(element.children[i], data, visited, false);
              }
            }
          }
        } else {
          element.setAttribute(propName, /** @type {string} */(value));
        }
      });
    }
  };

  var prefixes = [''];

  // Only look through shadow DOM when it's supported. As of April 2015, iOS
  // Chrome doesn't support shadow DOM.
  if (Element.prototype.createShadowRoot)
    prefixes.push('* /deep/ ');

  var attributeNames = Object.keys(handlers);
  var selector = prefixes.map(function(prefix) {
    return prefix + '[' + attributeNames.join('], ' + prefix + '[') + ']';
  }).join(', ');

  /**
   * Processes a DOM tree using a |data| source to populate template values.
   * @param {!ProcessingRoot} root The root of the DOM tree to process.
   * @param {!LoadTimeData} data The data to draw from.
   */
  function process(root, data) {
    processWithoutCycles(root, data, new Set(), true);
  }

  /**
   * Internal process() method that stops cycles while processing.
   * @param {!ProcessingRoot} root
   * @param {!LoadTimeData} data
   * @param {!Set<ProcessingRoot>} visited Already visited roots.
   * @param {boolean} mark Whether nodes should be marked processed.
   */
  function processWithoutCycles(root, data, visited, mark) {
    if (visited.has(root)) {
      // Found a cycle. Stop it.
      return;
    }

    // Mark the node as visited before recursing.
    visited.add(root);

    var importLinks = root.querySelectorAll('link[rel=import]');
    for (var i = 0; i < importLinks.length; ++i) {
      var importLink = /** @type {!HTMLLinkElement} */(importLinks[i]);
      if (!importLink.import) {
        // Happens when a <link rel=import> is inside a <template>.
        // TODO(dbeam): should we log an error if we detect that here?
        continue;
      }
      processWithoutCycles(importLink.import, data, visited, mark);
    }

    var templates = root.querySelectorAll('template');
    for (var i = 0; i < templates.length; ++i) {
      var template = /** @type {HTMLTemplateElement} */(templates[i]);
      if (!template.content)
        continue;
      processWithoutCycles(template.content, data, visited, mark);
    }

    var isElement = root instanceof Element;
    if (isElement && root.webkitMatchesSelector(selector))
      processElement(/** @type {!Element} */(root), data, visited);

    var elements = root.querySelectorAll(selector);
    for (var i = 0; i < elements.length; ++i) {
      processElement(elements[i], data, visited);
    }

    if (mark) {
      var processed = isElement ? [root] : root.children;
      if (processed) {
        for (var i = 0; i < processed.length; ++i) {
          processed[i].setAttribute('i18n-processed', '');
        }
      }
    }
  }

  /**
   * Run through various [i18n-*] attributes and populate.
   * @param {!Element} element
   * @param {!LoadTimeData} data
   * @param {!Set<ProcessingRoot>} visited
   */
  function processElement(element, data, visited) {
    for (var i = 0; i < attributeNames.length; i++) {
      var name = attributeNames[i];
      var attribute = element.getAttribute(name);
      if (attribute != null)
        handlers[name](element, attribute, data, visited);
    }
  }

  return {
    process: process
  };
}());


i18nTemplate.process(document, loadTimeData);
</script>`

// css from https://github.com/primer/markdown
var mdCss = `
/*!
 * GitHub User Content Stylesheets v2.5.0 (https://github.com/primer/markdown)
 * Copyright 2015 GitHub, Inc.
 * Licensed under MIT (https://github.com/primer/markdown/blob/master/LICENSE.md).
 */

articleMeta {
  background: #f7f7f7;
  color: #a2a2a2;
  border: 1px solid #ddd;
  width: 61.8%;
  margin-right:auto;
  margin-left:auto;
  display: block;
  margin-top: 30px;
  margin-bottom: 6px;
  padding-left: 30px;
  padding-right: 30px;
  font-family: 微软雅黑;
  padding-top:3px;
  padding-bottom:3px;
}

articleMeta a {
    color: #666;
    text-decoration: none
}

articleMeta i {
    margin-left:16px;
    margin-right:8px;
}

articleMeta .catalog {
    margin-right:12px;
}

articleMeta .raw {
  float: right;
  margin-top: 3px;
}


article {
  border: 1px solid #ddd;
  width: 61.8%;
  margin-right:auto;
  margin-left:auto;
  padding: 30px;
}

.markdown-body {
  font-family: "Helvetica Neue", Helvetica, "Segoe UI", Arial, freesans, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol";
  font-size: 16px;
  line-height: 1.6;
  word-wrap: break-word;
}
.markdown-body:before {
  display: table;
  content: "";
}
.markdown-body:after {
  display: table;
  clear: both;
  content: "";
}
.markdown-body > *:first-child {
  margin-top: 0 !important;
}
.markdown-body > *:last-child {
  margin-bottom: 0 !important;
}
.markdown-body a:not([href]) {
  color: inherit;
  text-decoration: none;
}
.markdown-body .absent {
  color: #c00;
}
.markdown-body .anchor {
  display: inline-block;
  padding-right: 2px;
  margin-left: -18px;
}
.markdown-body .anchor:focus {
  outline: none;
}
.markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4, .markdown-body h5, .markdown-body h6 {
  margin-top: 1em;
  margin-bottom: 16px;
  font-weight: bold;
  line-height: 1.4;
}
.markdown-body h1 .octicon-link, .markdown-body h2 .octicon-link, .markdown-body h3 .octicon-link, .markdown-body h4 .octicon-link, .markdown-body h5 .octicon-link, .markdown-body h6 .octicon-link {
  color: #000;
  vertical-align: middle;
  visibility: hidden;
}
.markdown-body h1:hover .anchor, .markdown-body h2:hover .anchor, .markdown-body h3:hover .anchor, .markdown-body h4:hover .anchor, .markdown-body h5:hover .anchor, .markdown-body h6:hover .anchor {
  text-decoration: none;
}
.markdown-body h1:hover .anchor .octicon-link, .markdown-body h2:hover .anchor .octicon-link, .markdown-body h3:hover .anchor .octicon-link, .markdown-body h4:hover .anchor .octicon-link, .markdown-body h5:hover .anchor .octicon-link, .markdown-body h6:hover .anchor .octicon-link {
  visibility: visible;
}
.markdown-body h1 tt,
    .markdown-body h1 code, .markdown-body h2 tt,
    .markdown-body h2 code, .markdown-body h3 tt,
    .markdown-body h3 code, .markdown-body h4 tt,
    .markdown-body h4 code, .markdown-body h5 tt,
    .markdown-body h5 code, .markdown-body h6 tt,
    .markdown-body h6 code {
  font-size: inherit;
}
.markdown-body h1 {
  padding-bottom: .3em;
  font-size: 2.25em;
  line-height: 1.2;
  border-bottom: 1px solid #eee;
}
.markdown-body h1 .anchor {
  line-height: 1;
}
.markdown-body h2 {
  padding-bottom: .3em;
  font-size: 1.75em;
  line-height: 1.225;
  border-bottom: 1px solid #eee;
}
.markdown-body h2 .anchor {
  line-height: 1;
}
.markdown-body h3 {
  font-size: 1.5em;
  line-height: 1.43;
}
.markdown-body h3 .anchor {
  line-height: 1.2;
}
.markdown-body h4 {
  font-size: 1.25em;
}
.markdown-body h4 .anchor {
  line-height: 1.2;
}
.markdown-body h5 {
  font-size: 1em;
}
.markdown-body h5 .anchor {
  line-height: 1.1;
}
.markdown-body h6 {
  font-size: 1em;
  color: #777;
}
.markdown-body h6 .anchor {
  line-height: 1.1;
}
.markdown-body p,
  .markdown-body blockquote,
  .markdown-body ul, .markdown-body ol, .markdown-body dl,
  .markdown-body table,
  .markdown-body pre {
  margin-top: 0;
  margin-bottom: 16px;
}
.markdown-body hr {
  height: 4px;
  padding: 0;
  margin: 16px 0;
  background-color: #e7e7e7;
  border: 0 none;
}
.markdown-body ul,
  .markdown-body ol {
  padding-left: 2em;
}
.markdown-body ul.no-list,
    .markdown-body ol.no-list {
  padding: 0;
  list-style-type: none;
}
.markdown-body ul ul,
  .markdown-body ul ol,
  .markdown-body ol ol,
  .markdown-body ol ul {
  margin-top: 0;
  margin-bottom: 0;
}
.markdown-body li > p {
  margin-top: 16px;
}
.markdown-body dl {
  padding: 0;
}
.markdown-body dl dt {
  padding: 0;
  margin-top: 16px;
  font-size: 1em;
  font-style: italic;
  font-weight: bold;
}
.markdown-body dl dd {
  padding: 0 16px;
  margin-bottom: 16px;
}
.markdown-body blockquote {
  padding: 0 15px;
  color: #777;
  border-left: 4px solid #ddd;
}
.markdown-body blockquote > :first-child {
  margin-top: 0;
}
.markdown-body blockquote > :last-child {
  margin-bottom: 0;
}
.markdown-body table {
  display: block;
  width: 100%;
  overflow: auto;
  word-break: normal;
  word-break: keep-all;
}
.markdown-body table th {
  font-weight: bold;
}
.markdown-body table th, .markdown-body table td {
  padding: 6px 13px;
  border: 1px solid #ddd;
}
.markdown-body table tr {
  background-color: #fff;
  border-top: 1px solid #ccc;
}
.markdown-body table tr:nth-child(2n) {
  background-color: #f8f8f8;
}
.markdown-body img {
  max-width: 100%;
  box-sizing: content-box;
  background-color: #fff;
}
.markdown-body img[align=right] {
  padding-left: 20px;
}
.markdown-body img[align=left] {
  padding-right: 20px;
}
.markdown-body .emoji {
  max-width: none;
}
.markdown-body span.frame {
  display: block;
  overflow: hidden;
}
.markdown-body span.frame > span {
  display: block;
  float: left;
  width: auto;
  padding: 7px;
  margin: 13px 0 0;
  overflow: hidden;
  border: 1px solid #ddd;
}
.markdown-body span.frame span img {
  display: block;
  float: left;
}
.markdown-body span.frame span span {
  display: block;
  padding: 5px 0 0;
  clear: both;
  color: #333;
}
.markdown-body span.align-center {
  display: block;
  overflow: hidden;
  clear: both;
}
.markdown-body span.align-center > span {
  display: block;
  margin: 13px auto 0;
  overflow: hidden;
  text-align: center;
}
.markdown-body span.align-center span img {
  margin: 0 auto;
  text-align: center;
}
.markdown-body span.align-right {
  display: block;
  overflow: hidden;
  clear: both;
}
.markdown-body span.align-right > span {
  display: block;
  margin: 13px 0 0;
  overflow: hidden;
  text-align: right;
}
.markdown-body span.align-right span img {
  margin: 0;
  text-align: right;
}
.markdown-body span.float-left {
  display: block;
  float: left;
  margin-right: 13px;
  overflow: hidden;
}
.markdown-body span.float-left span {
  margin: 13px 0 0;
}
.markdown-body span.float-right {
  display: block;
  float: right;
  margin-left: 13px;
  overflow: hidden;
}
.markdown-body span.float-right > span {
  display: block;
  margin: 13px auto 0;
  overflow: hidden;
  text-align: right;
}
.markdown-body code,
  .markdown-body tt {
  padding: 0;
  padding-top: .2em;
  padding-bottom: .2em;
  margin: 0;
  font-size: 85%;
  background-color: rgba(0, 0, 0, .04);
  border-radius: 3px;
  font-family: Consolas,Menlo,Monaco,Lucida Console,Liberation Mono,DejaVu Sans Mono,Bitstream Vera Sans Mono,Courier New,monospace,sans-serif;
}
.markdown-body code:before, .markdown-body code:after,
    .markdown-body tt:before,
    .markdown-body tt:after {
  letter-spacing: -.2em;
  content: "\00a0";
}
.markdown-body code br,
    .markdown-body tt br {
  display: none;
}
.markdown-body del code {
  text-decoration: inherit;
}
.markdown-body pre > code {
  padding: 0;
  margin: 0;
  font-size: 100%;
  word-break: normal;
  white-space: pre;
  background: transparent;
  border: 0;
}
.markdown-body .highlight {
  margin-bottom: 16px;
}
.markdown-body .highlight pre,
  .markdown-body pre {
  padding: 16px;
  overflow: auto;
  font-size: 85%;
  line-height: 1.45;
  background-color: #f7f7f7;
  border-radius: 3px;
}
.markdown-body .highlight pre {
  margin-bottom: 0;
  word-break: normal;
}
.markdown-body pre {
  word-wrap: normal;
}
.markdown-body pre code,
  .markdown-body pre tt {
  display: inline;
  max-width: initial;
  padding: 0;
  margin: 0;
  overflow: initial;
  line-height: inherit;
  word-wrap: normal;
  background-color: transparent;
  border: 0;
}
.markdown-body pre code:before, .markdown-body pre code:after,
    .markdown-body pre tt:before,
    .markdown-body pre tt:after {
  content: normal;
}
.markdown-body kbd {
  display: inline-block;
  padding: 3px 5px;
  font-size: 11px;
  line-height: 10px;
  color: #555;
  vertical-align: middle;
  background-color: #fcfcfc;
  border: solid 1px #ccc;
  border-bottom-color: #bbb;
  border-radius: 3px;
  box-shadow: inset 0 -1px 0 #bbb;
}

/*# sourceMappingURL=user-content.css.map */
`

// fontAwesome from http://fontawesome.io/
var faCss = `
/*!
 *  Font Awesome 4.6.3 by @davegandy - http://fontawesome.io - @fontawesome
 *  License - http://fontawesome.io/license (Font: SIL OFL 1.1, CSS: MIT License)
 */
/* FONT PATH
 * -------------------------- */
@font-face {
  font-family: 'FontAwesome';
  src: url("data:application/font-woff;base64,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") format("woff");
  font-weight: normal;
  font-style: normal;
}
.fa {
  display: inline-block;
  font: normal normal normal 14px/1 FontAwesome;
  font-size: inherit;
  text-rendering: auto;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
/* makes the font 33% larger relative to the icon container */
.fa-lg {
  font-size: 1.33333333em;
  line-height: 0.75em;
  vertical-align: -15%;
}
.fa-2x {
  font-size: 2em;
}
.fa-3x {
  font-size: 3em;
}
.fa-4x {
  font-size: 4em;
}
.fa-5x {
  font-size: 5em;
}
.fa-fw {
  width: 1.28571429em;
  text-align: center;
}
.fa-ul {
  padding-left: 0;
  margin-left: 2.14285714em;
  list-style-type: none;
}
.fa-ul > li {
  position: relative;
}
.fa-li {
  position: absolute;
  left: -2.14285714em;
  width: 2.14285714em;
  top: 0.14285714em;
  text-align: center;
}
.fa-li.fa-lg {
  left: -1.85714286em;
}
.fa-border {
  padding: .2em .25em .15em;
  border: solid 0.08em #eeeeee;
  border-radius: .1em;
}
.fa-pull-left {
  float: left;
}
.fa-pull-right {
  float: right;
}
.fa.fa-pull-left {
  margin-right: .3em;
}
.fa.fa-pull-right {
  margin-left: .3em;
}
/* Deprecated as of 4.4.0 */
.pull-right {
  float: right;
}
.pull-left {
  float: left;
}
.fa.pull-left {
  margin-right: .3em;
}
.fa.pull-right {
  margin-left: .3em;
}
.fa-spin {
  -webkit-animation: fa-spin 2s infinite linear;
  animation: fa-spin 2s infinite linear;
}
.fa-pulse {
  -webkit-animation: fa-spin 1s infinite steps(8);
  animation: fa-spin 1s infinite steps(8);
}
@-webkit-keyframes fa-spin {
  0% {
    -webkit-transform: rotate(0deg);
    transform: rotate(0deg);
  }
  100% {
    -webkit-transform: rotate(359deg);
    transform: rotate(359deg);
  }
}
@keyframes fa-spin {
  0% {
    -webkit-transform: rotate(0deg);
    transform: rotate(0deg);
  }
  100% {
    -webkit-transform: rotate(359deg);
    transform: rotate(359deg);
  }
}
.fa-rotate-90 {
  -ms-filter: "progid:DXImageTransform.Microsoft.BasicImage(rotation=1)";
  -webkit-transform: rotate(90deg);
  -ms-transform: rotate(90deg);
  transform: rotate(90deg);
}
.fa-rotate-180 {
  -ms-filter: "progid:DXImageTransform.Microsoft.BasicImage(rotation=2)";
  -webkit-transform: rotate(180deg);
  -ms-transform: rotate(180deg);
  transform: rotate(180deg);
}
.fa-rotate-270 {
  -ms-filter: "progid:DXImageTransform.Microsoft.BasicImage(rotation=3)";
  -webkit-transform: rotate(270deg);
  -ms-transform: rotate(270deg);
  transform: rotate(270deg);
}
.fa-flip-horizontal {
  -ms-filter: "progid:DXImageTransform.Microsoft.BasicImage(rotation=0, mirror=1)";
  -webkit-transform: scale(-1, 1);
  -ms-transform: scale(-1, 1);
  transform: scale(-1, 1);
}
.fa-flip-vertical {
  -ms-filter: "progid:DXImageTransform.Microsoft.BasicImage(rotation=2, mirror=1)";
  -webkit-transform: scale(1, -1);
  -ms-transform: scale(1, -1);
  transform: scale(1, -1);
}
:root .fa-rotate-90,
:root .fa-rotate-180,
:root .fa-rotate-270,
:root .fa-flip-horizontal,
:root .fa-flip-vertical {
  filter: none;
}
.fa-stack {
  position: relative;
  display: inline-block;
  width: 2em;
  height: 2em;
  line-height: 2em;
  vertical-align: middle;
}
.fa-stack-1x,
.fa-stack-2x {
  position: absolute;
  left: 0;
  width: 100%;
  text-align: center;
}
.fa-stack-1x {
  line-height: inherit;
}
.fa-stack-2x {
  font-size: 2em;
}
.fa-inverse {
  color: #ffffff;
}
/* Font Awesome uses the Unicode Private Use Area (PUA) to ensure screen
   readers do not read off random characters that represent icons */
.fa-glass:before {
  content: "\f000";
}
.fa-music:before {
  content: "\f001";
}
.fa-search:before {
  content: "\f002";
}
.fa-envelope-o:before {
  content: "\f003";
}
.fa-heart:before {
  content: "\f004";
}
.fa-star:before {
  content: "\f005";
}
.fa-star-o:before {
  content: "\f006";
}
.fa-user:before {
  content: "\f007";
}
.fa-film:before {
  content: "\f008";
}
.fa-th-large:before {
  content: "\f009";
}
.fa-th:before {
  content: "\f00a";
}
.fa-th-list:before {
  content: "\f00b";
}
.fa-check:before {
  content: "\f00c";
}
.fa-remove:before,
.fa-close:before,
.fa-times:before {
  content: "\f00d";
}
.fa-search-plus:before {
  content: "\f00e";
}
.fa-search-minus:before {
  content: "\f010";
}
.fa-power-off:before {
  content: "\f011";
}
.fa-signal:before {
  content: "\f012";
}
.fa-gear:before,
.fa-cog:before {
  content: "\f013";
}
.fa-trash-o:before {
  content: "\f014";
}
.fa-home:before {
  content: "\f015";
}
.fa-file-o:before {
  content: "\f016";
}
.fa-clock-o:before {
  content: "\f017";
}
.fa-road:before {
  content: "\f018";
}
.fa-download:before {
  content: "\f019";
}
.fa-arrow-circle-o-down:before {
  content: "\f01a";
}
.fa-arrow-circle-o-up:before {
  content: "\f01b";
}
.fa-inbox:before {
  content: "\f01c";
}
.fa-play-circle-o:before {
  content: "\f01d";
}
.fa-rotate-right:before,
.fa-repeat:before {
  content: "\f01e";
}
.fa-refresh:before {
  content: "\f021";
}
.fa-list-alt:before {
  content: "\f022";
}
.fa-lock:before {
  content: "\f023";
}
.fa-flag:before {
  content: "\f024";
}
.fa-headphones:before {
  content: "\f025";
}
.fa-volume-off:before {
  content: "\f026";
}
.fa-volume-down:before {
  content: "\f027";
}
.fa-volume-up:before {
  content: "\f028";
}
.fa-qrcode:before {
  content: "\f029";
}
.fa-barcode:before {
  content: "\f02a";
}
.fa-tag:before {
  content: "\f02b";
}
.fa-tags:before {
  content: "\f02c";
}
.fa-book:before {
  content: "\f02d";
}
.fa-bookmark:before {
  content: "\f02e";
}
.fa-print:before {
  content: "\f02f";
}
.fa-camera:before {
  content: "\f030";
}
.fa-font:before {
  content: "\f031";
}
.fa-bold:before {
  content: "\f032";
}
.fa-italic:before {
  content: "\f033";
}
.fa-text-height:before {
  content: "\f034";
}
.fa-text-width:before {
  content: "\f035";
}
.fa-align-left:before {
  content: "\f036";
}
.fa-align-center:before {
  content: "\f037";
}
.fa-align-right:before {
  content: "\f038";
}
.fa-align-justify:before {
  content: "\f039";
}
.fa-list:before {
  content: "\f03a";
}
.fa-dedent:before,
.fa-outdent:before {
  content: "\f03b";
}
.fa-indent:before {
  content: "\f03c";
}
.fa-video-camera:before {
  content: "\f03d";
}
.fa-photo:before,
.fa-image:before,
.fa-picture-o:before {
  content: "\f03e";
}
.fa-pencil:before {
  content: "\f040";
}
.fa-map-marker:before {
  content: "\f041";
}
.fa-adjust:before {
  content: "\f042";
}
.fa-tint:before {
  content: "\f043";
}
.fa-edit:before,
.fa-pencil-square-o:before {
  content: "\f044";
}
.fa-share-square-o:before {
  content: "\f045";
}
.fa-check-square-o:before {
  content: "\f046";
}
.fa-arrows:before {
  content: "\f047";
}
.fa-step-backward:before {
  content: "\f048";
}
.fa-fast-backward:before {
  content: "\f049";
}
.fa-backward:before {
  content: "\f04a";
}
.fa-play:before {
  content: "\f04b";
}
.fa-pause:before {
  content: "\f04c";
}
.fa-stop:before {
  content: "\f04d";
}
.fa-forward:before {
  content: "\f04e";
}
.fa-fast-forward:before {
  content: "\f050";
}
.fa-step-forward:before {
  content: "\f051";
}
.fa-eject:before {
  content: "\f052";
}
.fa-chevron-left:before {
  content: "\f053";
}
.fa-chevron-right:before {
  content: "\f054";
}
.fa-plus-circle:before {
  content: "\f055";
}
.fa-minus-circle:before {
  content: "\f056";
}
.fa-times-circle:before {
  content: "\f057";
}
.fa-check-circle:before {
  content: "\f058";
}
.fa-question-circle:before {
  content: "\f059";
}
.fa-info-circle:before {
  content: "\f05a";
}
.fa-crosshairs:before {
  content: "\f05b";
}
.fa-times-circle-o:before {
  content: "\f05c";
}
.fa-check-circle-o:before {
  content: "\f05d";
}
.fa-ban:before {
  content: "\f05e";
}
.fa-arrow-left:before {
  content: "\f060";
}
.fa-arrow-right:before {
  content: "\f061";
}
.fa-arrow-up:before {
  content: "\f062";
}
.fa-arrow-down:before {
  content: "\f063";
}
.fa-mail-forward:before,
.fa-share:before {
  content: "\f064";
}
.fa-expand:before {
  content: "\f065";
}
.fa-compress:before {
  content: "\f066";
}
.fa-plus:before {
  content: "\f067";
}
.fa-minus:before {
  content: "\f068";
}
.fa-asterisk:before {
  content: "\f069";
}
.fa-exclamation-circle:before {
  content: "\f06a";
}
.fa-gift:before {
  content: "\f06b";
}
.fa-leaf:before {
  content: "\f06c";
}
.fa-fire:before {
  content: "\f06d";
}
.fa-eye:before {
  content: "\f06e";
}
.fa-eye-slash:before {
  content: "\f070";
}
.fa-warning:before,
.fa-exclamation-triangle:before {
  content: "\f071";
}
.fa-plane:before {
  content: "\f072";
}
.fa-calendar:before {
  content: "\f073";
}
.fa-random:before {
  content: "\f074";
}
.fa-comment:before {
  content: "\f075";
}
.fa-magnet:before {
  content: "\f076";
}
.fa-chevron-up:before {
  content: "\f077";
}
.fa-chevron-down:before {
  content: "\f078";
}
.fa-retweet:before {
  content: "\f079";
}
.fa-shopping-cart:before {
  content: "\f07a";
}
.fa-folder:before {
  content: "\f07b";
}
.fa-folder-open:before {
  content: "\f07c";
}
.fa-arrows-v:before {
  content: "\f07d";
}
.fa-arrows-h:before {
  content: "\f07e";
}
.fa-bar-chart-o:before,
.fa-bar-chart:before {
  content: "\f080";
}
.fa-twitter-square:before {
  content: "\f081";
}
.fa-facebook-square:before {
  content: "\f082";
}
.fa-camera-retro:before {
  content: "\f083";
}
.fa-key:before {
  content: "\f084";
}
.fa-gears:before,
.fa-cogs:before {
  content: "\f085";
}
.fa-comments:before {
  content: "\f086";
}
.fa-thumbs-o-up:before {
  content: "\f087";
}
.fa-thumbs-o-down:before {
  content: "\f088";
}
.fa-star-half:before {
  content: "\f089";
}
.fa-heart-o:before {
  content: "\f08a";
}
.fa-sign-out:before {
  content: "\f08b";
}
.fa-linkedin-square:before {
  content: "\f08c";
}
.fa-thumb-tack:before {
  content: "\f08d";
}
.fa-external-link:before {
  content: "\f08e";
}
.fa-sign-in:before {
  content: "\f090";
}
.fa-trophy:before {
  content: "\f091";
}
.fa-github-square:before {
  content: "\f092";
}
.fa-upload:before {
  content: "\f093";
}
.fa-lemon-o:before {
  content: "\f094";
}
.fa-phone:before {
  content: "\f095";
}
.fa-square-o:before {
  content: "\f096";
}
.fa-bookmark-o:before {
  content: "\f097";
}
.fa-phone-square:before {
  content: "\f098";
}
.fa-twitter:before {
  content: "\f099";
}
.fa-facebook-f:before,
.fa-facebook:before {
  content: "\f09a";
}
.fa-github:before {
  content: "\f09b";
}
.fa-unlock:before {
  content: "\f09c";
}
.fa-credit-card:before {
  content: "\f09d";
}
.fa-feed:before,
.fa-rss:before {
  content: "\f09e";
}
.fa-hdd-o:before {
  content: "\f0a0";
}
.fa-bullhorn:before {
  content: "\f0a1";
}
.fa-bell:before {
  content: "\f0f3";
}
.fa-certificate:before {
  content: "\f0a3";
}
.fa-hand-o-right:before {
  content: "\f0a4";
}
.fa-hand-o-left:before {
  content: "\f0a5";
}
.fa-hand-o-up:before {
  content: "\f0a6";
}
.fa-hand-o-down:before {
  content: "\f0a7";
}
.fa-arrow-circle-left:before {
  content: "\f0a8";
}
.fa-arrow-circle-right:before {
  content: "\f0a9";
}
.fa-arrow-circle-up:before {
  content: "\f0aa";
}
.fa-arrow-circle-down:before {
  content: "\f0ab";
}
.fa-globe:before {
  content: "\f0ac";
}
.fa-wrench:before {
  content: "\f0ad";
}
.fa-tasks:before {
  content: "\f0ae";
}
.fa-filter:before {
  content: "\f0b0";
}
.fa-briefcase:before {
  content: "\f0b1";
}
.fa-arrows-alt:before {
  content: "\f0b2";
}
.fa-group:before,
.fa-users:before {
  content: "\f0c0";
}
.fa-chain:before,
.fa-link:before {
  content: "\f0c1";
}
.fa-cloud:before {
  content: "\f0c2";
}
.fa-flask:before {
  content: "\f0c3";
}
.fa-cut:before,
.fa-scissors:before {
  content: "\f0c4";
}
.fa-copy:before,
.fa-files-o:before {
  content: "\f0c5";
}
.fa-paperclip:before {
  content: "\f0c6";
}
.fa-save:before,
.fa-floppy-o:before {
  content: "\f0c7";
}
.fa-square:before {
  content: "\f0c8";
}
.fa-navicon:before,
.fa-reorder:before,
.fa-bars:before {
  content: "\f0c9";
}
.fa-list-ul:before {
  content: "\f0ca";
}
.fa-list-ol:before {
  content: "\f0cb";
}
.fa-strikethrough:before {
  content: "\f0cc";
}
.fa-underline:before {
  content: "\f0cd";
}
.fa-table:before {
  content: "\f0ce";
}
.fa-magic:before {
  content: "\f0d0";
}
.fa-truck:before {
  content: "\f0d1";
}
.fa-pinterest:before {
  content: "\f0d2";
}
.fa-pinterest-square:before {
  content: "\f0d3";
}
.fa-google-plus-square:before {
  content: "\f0d4";
}
.fa-google-plus:before {
  content: "\f0d5";
}
.fa-money:before {
  content: "\f0d6";
}
.fa-caret-down:before {
  content: "\f0d7";
}
.fa-caret-up:before {
  content: "\f0d8";
}
.fa-caret-left:before {
  content: "\f0d9";
}
.fa-caret-right:before {
  content: "\f0da";
}
.fa-columns:before {
  content: "\f0db";
}
.fa-unsorted:before,
.fa-sort:before {
  content: "\f0dc";
}
.fa-sort-down:before,
.fa-sort-desc:before {
  content: "\f0dd";
}
.fa-sort-up:before,
.fa-sort-asc:before {
  content: "\f0de";
}
.fa-envelope:before {
  content: "\f0e0";
}
.fa-linkedin:before {
  content: "\f0e1";
}
.fa-rotate-left:before,
.fa-undo:before {
  content: "\f0e2";
}
.fa-legal:before,
.fa-gavel:before {
  content: "\f0e3";
}
.fa-dashboard:before,
.fa-tachometer:before {
  content: "\f0e4";
}
.fa-comment-o:before {
  content: "\f0e5";
}
.fa-comments-o:before {
  content: "\f0e6";
}
.fa-flash:before,
.fa-bolt:before {
  content: "\f0e7";
}
.fa-sitemap:before {
  content: "\f0e8";
}
.fa-umbrella:before {
  content: "\f0e9";
}
.fa-paste:before,
.fa-clipboard:before {
  content: "\f0ea";
}
.fa-lightbulb-o:before {
  content: "\f0eb";
}
.fa-exchange:before {
  content: "\f0ec";
}
.fa-cloud-download:before {
  content: "\f0ed";
}
.fa-cloud-upload:before {
  content: "\f0ee";
}
.fa-user-md:before {
  content: "\f0f0";
}
.fa-stethoscope:before {
  content: "\f0f1";
}
.fa-suitcase:before {
  content: "\f0f2";
}
.fa-bell-o:before {
  content: "\f0a2";
}
.fa-coffee:before {
  content: "\f0f4";
}
.fa-cutlery:before {
  content: "\f0f5";
}
.fa-file-text-o:before {
  content: "\f0f6";
}
.fa-building-o:before {
  content: "\f0f7";
}
.fa-hospital-o:before {
  content: "\f0f8";
}
.fa-ambulance:before {
  content: "\f0f9";
}
.fa-medkit:before {
  content: "\f0fa";
}
.fa-fighter-jet:before {
  content: "\f0fb";
}
.fa-beer:before {
  content: "\f0fc";
}
.fa-h-square:before {
  content: "\f0fd";
}
.fa-plus-square:before {
  content: "\f0fe";
}
.fa-angle-double-left:before {
  content: "\f100";
}
.fa-angle-double-right:before {
  content: "\f101";
}
.fa-angle-double-up:before {
  content: "\f102";
}
.fa-angle-double-down:before {
  content: "\f103";
}
.fa-angle-left:before {
  content: "\f104";
}
.fa-angle-right:before {
  content: "\f105";
}
.fa-angle-up:before {
  content: "\f106";
}
.fa-angle-down:before {
  content: "\f107";
}
.fa-desktop:before {
  content: "\f108";
}
.fa-laptop:before {
  content: "\f109";
}
.fa-tablet:before {
  content: "\f10a";
}
.fa-mobile-phone:before,
.fa-mobile:before {
  content: "\f10b";
}
.fa-circle-o:before {
  content: "\f10c";
}
.fa-quote-left:before {
  content: "\f10d";
}
.fa-quote-right:before {
  content: "\f10e";
}
.fa-spinner:before {
  content: "\f110";
}
.fa-circle:before {
  content: "\f111";
}
.fa-mail-reply:before,
.fa-reply:before {
  content: "\f112";
}
.fa-github-alt:before {
  content: "\f113";
}
.fa-folder-o:before {
  content: "\f114";
}
.fa-folder-open-o:before {
  content: "\f115";
}
.fa-smile-o:before {
  content: "\f118";
}
.fa-frown-o:before {
  content: "\f119";
}
.fa-meh-o:before {
  content: "\f11a";
}
.fa-gamepad:before {
  content: "\f11b";
}
.fa-keyboard-o:before {
  content: "\f11c";
}
.fa-flag-o:before {
  content: "\f11d";
}
.fa-flag-checkered:before {
  content: "\f11e";
}
.fa-terminal:before {
  content: "\f120";
}
.fa-code:before {
  content: "\f121";
}
.fa-mail-reply-all:before,
.fa-reply-all:before {
  content: "\f122";
}
.fa-star-half-empty:before,
.fa-star-half-full:before,
.fa-star-half-o:before {
  content: "\f123";
}
.fa-location-arrow:before {
  content: "\f124";
}
.fa-crop:before {
  content: "\f125";
}
.fa-code-fork:before {
  content: "\f126";
}
.fa-unlink:before,
.fa-chain-broken:before {
  content: "\f127";
}
.fa-question:before {
  content: "\f128";
}
.fa-info:before {
  content: "\f129";
}
.fa-exclamation:before {
  content: "\f12a";
}
.fa-superscript:before {
  content: "\f12b";
}
.fa-subscript:before {
  content: "\f12c";
}
.fa-eraser:before {
  content: "\f12d";
}
.fa-puzzle-piece:before {
  content: "\f12e";
}
.fa-microphone:before {
  content: "\f130";
}
.fa-microphone-slash:before {
  content: "\f131";
}
.fa-shield:before {
  content: "\f132";
}
.fa-calendar-o:before {
  content: "\f133";
}
.fa-fire-extinguisher:before {
  content: "\f134";
}
.fa-rocket:before {
  content: "\f135";
}
.fa-maxcdn:before {
  content: "\f136";
}
.fa-chevron-circle-left:before {
  content: "\f137";
}
.fa-chevron-circle-right:before {
  content: "\f138";
}
.fa-chevron-circle-up:before {
  content: "\f139";
}
.fa-chevron-circle-down:before {
  content: "\f13a";
}
.fa-html5:before {
  content: "\f13b";
}
.fa-css3:before {
  content: "\f13c";
}
.fa-anchor:before {
  content: "\f13d";
}
.fa-unlock-alt:before {
  content: "\f13e";
}
.fa-bullseye:before {
  content: "\f140";
}
.fa-ellipsis-h:before {
  content: "\f141";
}
.fa-ellipsis-v:before {
  content: "\f142";
}
.fa-rss-square:before {
  content: "\f143";
}
.fa-play-circle:before {
  content: "\f144";
}
.fa-ticket:before {
  content: "\f145";
}
.fa-minus-square:before {
  content: "\f146";
}
.fa-minus-square-o:before {
  content: "\f147";
}
.fa-level-up:before {
  content: "\f148";
}
.fa-level-down:before {
  content: "\f149";
}
.fa-check-square:before {
  content: "\f14a";
}
.fa-pencil-square:before {
  content: "\f14b";
}
.fa-external-link-square:before {
  content: "\f14c";
}
.fa-share-square:before {
  content: "\f14d";
}
.fa-compass:before {
  content: "\f14e";
}
.fa-toggle-down:before,
.fa-caret-square-o-down:before {
  content: "\f150";
}
.fa-toggle-up:before,
.fa-caret-square-o-up:before {
  content: "\f151";
}
.fa-toggle-right:before,
.fa-caret-square-o-right:before {
  content: "\f152";
}
.fa-euro:before,
.fa-eur:before {
  content: "\f153";
}
.fa-gbp:before {
  content: "\f154";
}
.fa-dollar:before,
.fa-usd:before {
  content: "\f155";
}
.fa-rupee:before,
.fa-inr:before {
  content: "\f156";
}
.fa-cny:before,
.fa-rmb:before,
.fa-yen:before,
.fa-jpy:before {
  content: "\f157";
}
.fa-ruble:before,
.fa-rouble:before,
.fa-rub:before {
  content: "\f158";
}
.fa-won:before,
.fa-krw:before {
  content: "\f159";
}
.fa-bitcoin:before,
.fa-btc:before {
  content: "\f15a";
}
.fa-file:before {
  content: "\f15b";
}
.fa-file-text:before {
  content: "\f15c";
}
.fa-sort-alpha-asc:before {
  content: "\f15d";
}
.fa-sort-alpha-desc:before {
  content: "\f15e";
}
.fa-sort-amount-asc:before {
  content: "\f160";
}
.fa-sort-amount-desc:before {
  content: "\f161";
}
.fa-sort-numeric-asc:before {
  content: "\f162";
}
.fa-sort-numeric-desc:before {
  content: "\f163";
}
.fa-thumbs-up:before {
  content: "\f164";
}
.fa-thumbs-down:before {
  content: "\f165";
}
.fa-youtube-square:before {
  content: "\f166";
}
.fa-youtube:before {
  content: "\f167";
}
.fa-xing:before {
  content: "\f168";
}
.fa-xing-square:before {
  content: "\f169";
}
.fa-youtube-play:before {
  content: "\f16a";
}
.fa-dropbox:before {
  content: "\f16b";
}
.fa-stack-overflow:before {
  content: "\f16c";
}
.fa-instagram:before {
  content: "\f16d";
}
.fa-flickr:before {
  content: "\f16e";
}
.fa-adn:before {
  content: "\f170";
}
.fa-bitbucket:before {
  content: "\f171";
}
.fa-bitbucket-square:before {
  content: "\f172";
}
.fa-tumblr:before {
  content: "\f173";
}
.fa-tumblr-square:before {
  content: "\f174";
}
.fa-long-arrow-down:before {
  content: "\f175";
}
.fa-long-arrow-up:before {
  content: "\f176";
}
.fa-long-arrow-left:before {
  content: "\f177";
}
.fa-long-arrow-right:before {
  content: "\f178";
}
.fa-apple:before {
  content: "\f179";
}
.fa-windows:before {
  content: "\f17a";
}
.fa-android:before {
  content: "\f17b";
}
.fa-linux:before {
  content: "\f17c";
}
.fa-dribbble:before {
  content: "\f17d";
}
.fa-skype:before {
  content: "\f17e";
}
.fa-foursquare:before {
  content: "\f180";
}
.fa-trello:before {
  content: "\f181";
}
.fa-female:before {
  content: "\f182";
}
.fa-male:before {
  content: "\f183";
}
.fa-gittip:before,
.fa-gratipay:before {
  content: "\f184";
}
.fa-sun-o:before {
  content: "\f185";
}
.fa-moon-o:before {
  content: "\f186";
}
.fa-archive:before {
  content: "\f187";
}
.fa-bug:before {
  content: "\f188";
}
.fa-vk:before {
  content: "\f189";
}
.fa-weibo:before {
  content: "\f18a";
}
.fa-renren:before {
  content: "\f18b";
}
.fa-pagelines:before {
  content: "\f18c";
}
.fa-stack-exchange:before {
  content: "\f18d";
}
.fa-arrow-circle-o-right:before {
  content: "\f18e";
}
.fa-arrow-circle-o-left:before {
  content: "\f190";
}
.fa-toggle-left:before,
.fa-caret-square-o-left:before {
  content: "\f191";
}
.fa-dot-circle-o:before {
  content: "\f192";
}
.fa-wheelchair:before {
  content: "\f193";
}
.fa-vimeo-square:before {
  content: "\f194";
}
.fa-turkish-lira:before,
.fa-try:before {
  content: "\f195";
}
.fa-plus-square-o:before {
  content: "\f196";
}
.fa-space-shuttle:before {
  content: "\f197";
}
.fa-slack:before {
  content: "\f198";
}
.fa-envelope-square:before {
  content: "\f199";
}
.fa-wordpress:before {
  content: "\f19a";
}
.fa-openid:before {
  content: "\f19b";
}
.fa-institution:before,
.fa-bank:before,
.fa-university:before {
  content: "\f19c";
}
.fa-mortar-board:before,
.fa-graduation-cap:before {
  content: "\f19d";
}
.fa-yahoo:before {
  content: "\f19e";
}
.fa-google:before {
  content: "\f1a0";
}
.fa-reddit:before {
  content: "\f1a1";
}
.fa-reddit-square:before {
  content: "\f1a2";
}
.fa-stumbleupon-circle:before {
  content: "\f1a3";
}
.fa-stumbleupon:before {
  content: "\f1a4";
}
.fa-delicious:before {
  content: "\f1a5";
}
.fa-digg:before {
  content: "\f1a6";
}
.fa-pied-piper-pp:before {
  content: "\f1a7";
}
.fa-pied-piper-alt:before {
  content: "\f1a8";
}
.fa-drupal:before {
  content: "\f1a9";
}
.fa-joomla:before {
  content: "\f1aa";
}
.fa-language:before {
  content: "\f1ab";
}
.fa-fax:before {
  content: "\f1ac";
}
.fa-building:before {
  content: "\f1ad";
}
.fa-child:before {
  content: "\f1ae";
}
.fa-paw:before {
  content: "\f1b0";
}
.fa-spoon:before {
  content: "\f1b1";
}
.fa-cube:before {
  content: "\f1b2";
}
.fa-cubes:before {
  content: "\f1b3";
}
.fa-behance:before {
  content: "\f1b4";
}
.fa-behance-square:before {
  content: "\f1b5";
}
.fa-steam:before {
  content: "\f1b6";
}
.fa-steam-square:before {
  content: "\f1b7";
}
.fa-recycle:before {
  content: "\f1b8";
}
.fa-automobile:before,
.fa-car:before {
  content: "\f1b9";
}
.fa-cab:before,
.fa-taxi:before {
  content: "\f1ba";
}
.fa-tree:before {
  content: "\f1bb";
}
.fa-spotify:before {
  content: "\f1bc";
}
.fa-deviantart:before {
  content: "\f1bd";
}
.fa-soundcloud:before {
  content: "\f1be";
}
.fa-database:before {
  content: "\f1c0";
}
.fa-file-pdf-o:before {
  content: "\f1c1";
}
.fa-file-word-o:before {
  content: "\f1c2";
}
.fa-file-excel-o:before {
  content: "\f1c3";
}
.fa-file-powerpoint-o:before {
  content: "\f1c4";
}
.fa-file-photo-o:before,
.fa-file-picture-o:before,
.fa-file-image-o:before {
  content: "\f1c5";
}
.fa-file-zip-o:before,
.fa-file-archive-o:before {
  content: "\f1c6";
}
.fa-file-sound-o:before,
.fa-file-audio-o:before {
  content: "\f1c7";
}
.fa-file-movie-o:before,
.fa-file-video-o:before {
  content: "\f1c8";
}
.fa-file-code-o:before {
  content: "\f1c9";
}
.fa-vine:before {
  content: "\f1ca";
}
.fa-codepen:before {
  content: "\f1cb";
}
.fa-jsfiddle:before {
  content: "\f1cc";
}
.fa-life-bouy:before,
.fa-life-buoy:before,
.fa-life-saver:before,
.fa-support:before,
.fa-life-ring:before {
  content: "\f1cd";
}
.fa-circle-o-notch:before {
  content: "\f1ce";
}
.fa-ra:before,
.fa-resistance:before,
.fa-rebel:before {
  content: "\f1d0";
}
.fa-ge:before,
.fa-empire:before {
  content: "\f1d1";
}
.fa-git-square:before {
  content: "\f1d2";
}
.fa-git:before {
  content: "\f1d3";
}
.fa-y-combinator-square:before,
.fa-yc-square:before,
.fa-hacker-news:before {
  content: "\f1d4";
}
.fa-tencent-weibo:before {
  content: "\f1d5";
}
.fa-qq:before {
  content: "\f1d6";
}
.fa-wechat:before,
.fa-weixin:before {
  content: "\f1d7";
}
.fa-send:before,
.fa-paper-plane:before {
  content: "\f1d8";
}
.fa-send-o:before,
.fa-paper-plane-o:before {
  content: "\f1d9";
}
.fa-history:before {
  content: "\f1da";
}
.fa-circle-thin:before {
  content: "\f1db";
}
.fa-header:before {
  content: "\f1dc";
}
.fa-paragraph:before {
  content: "\f1dd";
}
.fa-sliders:before {
  content: "\f1de";
}
.fa-share-alt:before {
  content: "\f1e0";
}
.fa-share-alt-square:before {
  content: "\f1e1";
}
.fa-bomb:before {
  content: "\f1e2";
}
.fa-soccer-ball-o:before,
.fa-futbol-o:before {
  content: "\f1e3";
}
.fa-tty:before {
  content: "\f1e4";
}
.fa-binoculars:before {
  content: "\f1e5";
}
.fa-plug:before {
  content: "\f1e6";
}
.fa-slideshare:before {
  content: "\f1e7";
}
.fa-twitch:before {
  content: "\f1e8";
}
.fa-yelp:before {
  content: "\f1e9";
}
.fa-newspaper-o:before {
  content: "\f1ea";
}
.fa-wifi:before {
  content: "\f1eb";
}
.fa-calculator:before {
  content: "\f1ec";
}
.fa-paypal:before {
  content: "\f1ed";
}
.fa-google-wallet:before {
  content: "\f1ee";
}
.fa-cc-visa:before {
  content: "\f1f0";
}
.fa-cc-mastercard:before {
  content: "\f1f1";
}
.fa-cc-discover:before {
  content: "\f1f2";
}
.fa-cc-amex:before {
  content: "\f1f3";
}
.fa-cc-paypal:before {
  content: "\f1f4";
}
.fa-cc-stripe:before {
  content: "\f1f5";
}
.fa-bell-slash:before {
  content: "\f1f6";
}
.fa-bell-slash-o:before {
  content: "\f1f7";
}
.fa-trash:before {
  content: "\f1f8";
}
.fa-copyright:before {
  content: "\f1f9";
}
.fa-at:before {
  content: "\f1fa";
}
.fa-eyedropper:before {
  content: "\f1fb";
}
.fa-paint-brush:before {
  content: "\f1fc";
}
.fa-birthday-cake:before {
  content: "\f1fd";
}
.fa-area-chart:before {
  content: "\f1fe";
}
.fa-pie-chart:before {
  content: "\f200";
}
.fa-line-chart:before {
  content: "\f201";
}
.fa-lastfm:before {
  content: "\f202";
}
.fa-lastfm-square:before {
  content: "\f203";
}
.fa-toggle-off:before {
  content: "\f204";
}
.fa-toggle-on:before {
  content: "\f205";
}
.fa-bicycle:before {
  content: "\f206";
}
.fa-bus:before {
  content: "\f207";
}
.fa-ioxhost:before {
  content: "\f208";
}
.fa-angellist:before {
  content: "\f209";
}
.fa-cc:before {
  content: "\f20a";
}
.fa-shekel:before,
.fa-sheqel:before,
.fa-ils:before {
  content: "\f20b";
}
.fa-meanpath:before {
  content: "\f20c";
}
.fa-buysellads:before {
  content: "\f20d";
}
.fa-connectdevelop:before {
  content: "\f20e";
}
.fa-dashcube:before {
  content: "\f210";
}
.fa-forumbee:before {
  content: "\f211";
}
.fa-leanpub:before {
  content: "\f212";
}
.fa-sellsy:before {
  content: "\f213";
}
.fa-shirtsinbulk:before {
  content: "\f214";
}
.fa-simplybuilt:before {
  content: "\f215";
}
.fa-skyatlas:before {
  content: "\f216";
}
.fa-cart-plus:before {
  content: "\f217";
}
.fa-cart-arrow-down:before {
  content: "\f218";
}
.fa-diamond:before {
  content: "\f219";
}
.fa-ship:before {
  content: "\f21a";
}
.fa-user-secret:before {
  content: "\f21b";
}
.fa-motorcycle:before {
  content: "\f21c";
}
.fa-street-view:before {
  content: "\f21d";
}
.fa-heartbeat:before {
  content: "\f21e";
}
.fa-venus:before {
  content: "\f221";
}
.fa-mars:before {
  content: "\f222";
}
.fa-mercury:before {
  content: "\f223";
}
.fa-intersex:before,
.fa-transgender:before {
  content: "\f224";
}
.fa-transgender-alt:before {
  content: "\f225";
}
.fa-venus-double:before {
  content: "\f226";
}
.fa-mars-double:before {
  content: "\f227";
}
.fa-venus-mars:before {
  content: "\f228";
}
.fa-mars-stroke:before {
  content: "\f229";
}
.fa-mars-stroke-v:before {
  content: "\f22a";
}
.fa-mars-stroke-h:before {
  content: "\f22b";
}
.fa-neuter:before {
  content: "\f22c";
}
.fa-genderless:before {
  content: "\f22d";
}
.fa-facebook-official:before {
  content: "\f230";
}
.fa-pinterest-p:before {
  content: "\f231";
}
.fa-whatsapp:before {
  content: "\f232";
}
.fa-server:before {
  content: "\f233";
}
.fa-user-plus:before {
  content: "\f234";
}
.fa-user-times:before {
  content: "\f235";
}
.fa-hotel:before,
.fa-bed:before {
  content: "\f236";
}
.fa-viacoin:before {
  content: "\f237";
}
.fa-train:before {
  content: "\f238";
}
.fa-subway:before {
  content: "\f239";
}
.fa-medium:before {
  content: "\f23a";
}
.fa-yc:before,
.fa-y-combinator:before {
  content: "\f23b";
}
.fa-optin-monster:before {
  content: "\f23c";
}
.fa-opencart:before {
  content: "\f23d";
}
.fa-expeditedssl:before {
  content: "\f23e";
}
.fa-battery-4:before,
.fa-battery-full:before {
  content: "\f240";
}
.fa-battery-3:before,
.fa-battery-three-quarters:before {
  content: "\f241";
}
.fa-battery-2:before,
.fa-battery-half:before {
  content: "\f242";
}
.fa-battery-1:before,
.fa-battery-quarter:before {
  content: "\f243";
}
.fa-battery-0:before,
.fa-battery-empty:before {
  content: "\f244";
}
.fa-mouse-pointer:before {
  content: "\f245";
}
.fa-i-cursor:before {
  content: "\f246";
}
.fa-object-group:before {
  content: "\f247";
}
.fa-object-ungroup:before {
  content: "\f248";
}
.fa-sticky-note:before {
  content: "\f249";
}
.fa-sticky-note-o:before {
  content: "\f24a";
}
.fa-cc-jcb:before {
  content: "\f24b";
}
.fa-cc-diners-club:before {
  content: "\f24c";
}
.fa-clone:before {
  content: "\f24d";
}
.fa-balance-scale:before {
  content: "\f24e";
}
.fa-hourglass-o:before {
  content: "\f250";
}
.fa-hourglass-1:before,
.fa-hourglass-start:before {
  content: "\f251";
}
.fa-hourglass-2:before,
.fa-hourglass-half:before {
  content: "\f252";
}
.fa-hourglass-3:before,
.fa-hourglass-end:before {
  content: "\f253";
}
.fa-hourglass:before {
  content: "\f254";
}
.fa-hand-grab-o:before,
.fa-hand-rock-o:before {
  content: "\f255";
}
.fa-hand-stop-o:before,
.fa-hand-paper-o:before {
  content: "\f256";
}
.fa-hand-scissors-o:before {
  content: "\f257";
}
.fa-hand-lizard-o:before {
  content: "\f258";
}
.fa-hand-spock-o:before {
  content: "\f259";
}
.fa-hand-pointer-o:before {
  content: "\f25a";
}
.fa-hand-peace-o:before {
  content: "\f25b";
}
.fa-trademark:before {
  content: "\f25c";
}
.fa-registered:before {
  content: "\f25d";
}
.fa-creative-commons:before {
  content: "\f25e";
}
.fa-gg:before {
  content: "\f260";
}
.fa-gg-circle:before {
  content: "\f261";
}
.fa-tripadvisor:before {
  content: "\f262";
}
.fa-odnoklassniki:before {
  content: "\f263";
}
.fa-odnoklassniki-square:before {
  content: "\f264";
}
.fa-get-pocket:before {
  content: "\f265";
}
.fa-wikipedia-w:before {
  content: "\f266";
}
.fa-safari:before {
  content: "\f267";
}
.fa-chrome:before {
  content: "\f268";
}
.fa-firefox:before {
  content: "\f269";
}
.fa-opera:before {
  content: "\f26a";
}
.fa-internet-explorer:before {
  content: "\f26b";
}
.fa-tv:before,
.fa-television:before {
  content: "\f26c";
}
.fa-contao:before {
  content: "\f26d";
}
.fa-500px:before {
  content: "\f26e";
}
.fa-amazon:before {
  content: "\f270";
}
.fa-calendar-plus-o:before {
  content: "\f271";
}
.fa-calendar-minus-o:before {
  content: "\f272";
}
.fa-calendar-times-o:before {
  content: "\f273";
}
.fa-calendar-check-o:before {
  content: "\f274";
}
.fa-industry:before {
  content: "\f275";
}
.fa-map-pin:before {
  content: "\f276";
}
.fa-map-signs:before {
  content: "\f277";
}
.fa-map-o:before {
  content: "\f278";
}
.fa-map:before {
  content: "\f279";
}
.fa-commenting:before {
  content: "\f27a";
}
.fa-commenting-o:before {
  content: "\f27b";
}
.fa-houzz:before {
  content: "\f27c";
}
.fa-vimeo:before {
  content: "\f27d";
}
.fa-black-tie:before {
  content: "\f27e";
}
.fa-fonticons:before {
  content: "\f280";
}
.fa-reddit-alien:before {
  content: "\f281";
}
.fa-edge:before {
  content: "\f282";
}
.fa-credit-card-alt:before {
  content: "\f283";
}
.fa-codiepie:before {
  content: "\f284";
}
.fa-modx:before {
  content: "\f285";
}
.fa-fort-awesome:before {
  content: "\f286";
}
.fa-usb:before {
  content: "\f287";
}
.fa-product-hunt:before {
  content: "\f288";
}
.fa-mixcloud:before {
  content: "\f289";
}
.fa-scribd:before {
  content: "\f28a";
}
.fa-pause-circle:before {
  content: "\f28b";
}
.fa-pause-circle-o:before {
  content: "\f28c";
}
.fa-stop-circle:before {
  content: "\f28d";
}
.fa-stop-circle-o:before {
  content: "\f28e";
}
.fa-shopping-bag:before {
  content: "\f290";
}
.fa-shopping-basket:before {
  content: "\f291";
}
.fa-hashtag:before {
  content: "\f292";
}
.fa-bluetooth:before {
  content: "\f293";
}
.fa-bluetooth-b:before {
  content: "\f294";
}
.fa-percent:before {
  content: "\f295";
}
.fa-gitlab:before {
  content: "\f296";
}
.fa-wpbeginner:before {
  content: "\f297";
}
.fa-wpforms:before {
  content: "\f298";
}
.fa-envira:before {
  content: "\f299";
}
.fa-universal-access:before {
  content: "\f29a";
}
.fa-wheelchair-alt:before {
  content: "\f29b";
}
.fa-question-circle-o:before {
  content: "\f29c";
}
.fa-blind:before {
  content: "\f29d";
}
.fa-audio-description:before {
  content: "\f29e";
}
.fa-volume-control-phone:before {
  content: "\f2a0";
}
.fa-braille:before {
  content: "\f2a1";
}
.fa-assistive-listening-systems:before {
  content: "\f2a2";
}
.fa-asl-interpreting:before,
.fa-american-sign-language-interpreting:before {
  content: "\f2a3";
}
.fa-deafness:before,
.fa-hard-of-hearing:before,
.fa-deaf:before {
  content: "\f2a4";
}
.fa-glide:before {
  content: "\f2a5";
}
.fa-glide-g:before {
  content: "\f2a6";
}
.fa-signing:before,
.fa-sign-language:before {
  content: "\f2a7";
}
.fa-low-vision:before {
  content: "\f2a8";
}
.fa-viadeo:before {
  content: "\f2a9";
}
.fa-viadeo-square:before {
  content: "\f2aa";
}
.fa-snapchat:before {
  content: "\f2ab";
}
.fa-snapchat-ghost:before {
  content: "\f2ac";
}
.fa-snapchat-square:before {
  content: "\f2ad";
}
.fa-pied-piper:before {
  content: "\f2ae";
}
.fa-first-order:before {
  content: "\f2b0";
}
.fa-yoast:before {
  content: "\f2b1";
}
.fa-themeisle:before {
  content: "\f2b2";
}
.fa-google-plus-circle:before,
.fa-google-plus-official:before {
  content: "\f2b3";
}
.fa-fa:before,
.fa-font-awesome:before {
  content: "\f2b4";
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  border: 0;
}
.sr-only-focusable:active,
.sr-only-focusable:focus {
  position: static;
  width: auto;
  height: auto;
  margin: 0;
  overflow: visible;
  clip: auto;
}
`
