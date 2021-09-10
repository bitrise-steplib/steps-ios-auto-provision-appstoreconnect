require_relative 'portal/auth_client'
require_relative 'certificate_helper'
require_relative 'profile'
require_relative 'app'
require_relative 'log'
require 'optparse'

begin
  options = {}
  OptionParser.new do |opt|
    opt.on('--username USERNAME') { |o| options[:username] = o }
    opt.on('--password PASSWORD') { |o| options[:password] = o }
    opt.on('--subcommand SUBCOMMAND') { |o| options[:subcommand] = o }
    opt.on('--bundle_id BUNDLE_ID') { |o| options[:bundle_id] = o }
    opt.on('--id ID') { |o| options[:id] = o }
    opt.on('--name NAME') { |o| options[:name] = o }
    opt.on('--certificate CERTIFICATE') { |o| options[:certificate] = o }
    opt.on('--profile_name PROFILE_NAME') { |o| options[:profile_name] = o }
    opt.on('--profile-type PROFILE_TYPE') { |o| options[:profile_type] = o }
  end.parse!

  Log.verbose = true

  Portal::AuthClient.login(options[:username], options[:password])
  Log.info('logged in')

  result = '{}'
  case options[:subcommand]
  when 'list_dev_certs'
    client = CertificateHelper.new
    result = client.list_dev_certs
  when 'list_dist_certs'
    client = CertificateHelper.new
    result = client.list_dist_certs
  when 'list_profiles'
    result = list_profiles(options[:profile_type], options[:name])
  when 'get_app'
    result = get_app(options[:bundle_id])
  when 'delete_profile'
    delete_profile(options[:id])
    result = { status: 'OK' }
  when 'create_profile'
    result = create_profile(options[:profile_type], options[:bundle_id], options[:certificate], options[:profile_name])
  end

  response = { data: result }
  puts response.to_json.to_s
rescue => e
  result = { error: "#{e}, stacktrace: #{e.backtrace.join("\n")}" }
  puts result.to_json.to_s
end
